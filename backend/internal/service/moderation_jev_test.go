package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/applemango/chain/internal/external/nanogpt"
	"github.com/applemango/chain/internal/service"
)

// 11 §5.3 のしきい値と、11 §5.4 のエラー扱い。NanoGPT は httptest で偽装し、外部に出ない。

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "nanogpt", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

// newJevServer は決まった status / body を返す偽 NanoGPT を立て、最後のリクエスト本文を記録する。
func newJevServer(t *testing.T, status int, body []byte) (*httptest.Server, *[]byte) {
	t.Helper()
	var last []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		last = b
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer ...", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &last
}

func newJev(t *testing.T, srv *httptest.Server) *service.Jev {
	t.Helper()
	return &service.Jev{Client: nanogpt.NewClient(srv.URL, "test-key", "typesafe/jev-1.13")}
}

func TestJevThresholds(t *testing.T) {
	cases := []struct {
		fixture    string
		label      service.Label
		categories []string
		score      float64
		pin        bool
	}{
		{"ok.json", service.LabelOK, []string{}, 0.03, true},
		{"warn.json", service.LabelWarn, []string{"harassment", "spam"}, 0.65, false},
		{"block.json", service.LabelBlock, []string{"hate"}, 0.92, false},
		{"pin.json", service.LabelOK, []string{"personal_info"}, 0.07, true},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			srv, _ := newJevServer(t, http.StatusOK, fixture(t, tc.fixture))
			jev := newJev(t, srv)

			v, pin, err := jev.Assess(context.Background(), service.Content{Kind: service.ContentKindInstancePost, Text: "明日の集合は 9:00 正門"})
			if err != nil {
				t.Fatalf("Assess: %v", err)
			}
			if v.Label != tc.label {
				t.Fatalf("label = %q, want %q", v.Label, tc.label)
			}
			if !reflect.DeepEqual(v.Categories, tc.categories) {
				t.Fatalf("categories = %#v, want %#v", v.Categories, tc.categories)
			}
			if math.Abs(v.Score-tc.score) > 1e-9 {
				t.Fatalf("score = %v, want %v", v.Score, tc.score)
			}
			if pin != tc.pin {
				t.Fatalf("pin = %v, want %v", pin, tc.pin)
			}
		})
	}
}

// グループ名などの判定は pin を付けない（11 §5.2）。
func TestJevJudgeHasNoPinQuestion(t *testing.T) {
	srv, body := newJevServer(t, http.StatusOK, fixture(t, "block.json"))
	jev := newJev(t, srv)

	v, err := jev.Judge(context.Background(), service.Content{Kind: "group_name", Text: "ばか"})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if v.Label != service.LabelBlock {
		t.Fatalf("label = %q, want block", v.Label)
	}

	var sent struct {
		Questions map[string]json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(*body, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if _, ok := sent.Questions["pin"]; ok {
		t.Fatalf("Judge sent a pin question (body=%s)", *body)
	}
	if _, ok := sent.Questions["safety"]; !ok {
		t.Fatalf("Judge did not send safety (body=%s)", *body)
	}
}

// 送る本文には state の kind と text だけを入れ、DID・handle・Space 名を入れない（02 §8、11 §5.2）。
func TestJevRequestContainsNoIdentity(t *testing.T) {
	srv, body := newJevServer(t, http.StatusOK, fixture(t, "ok.json"))
	jev := newJev(t, srv)

	if _, _, err := jev.Assess(context.Background(), service.Content{Kind: service.ContentKindInstancePost, Text: "本文"}); err != nil {
		t.Fatalf("Assess: %v", err)
	}

	var sent struct {
		Model     string                     `json:"model"`
		State     map[string]json.RawMessage `json:"state"`
		Questions map[string]json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(*body, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.Model != "typesafe/jev-1.13" {
		t.Fatalf("model = %q, want the client model", sent.Model)
	}
	if len(sent.State) != 2 {
		t.Fatalf("state keys = %v, want exactly kind and text", sent.State)
	}
	if _, ok := sent.State["kind"]; !ok {
		t.Fatalf("state has no kind: %s", *body)
	}
	if _, ok := sent.State["text"]; !ok {
		t.Fatalf("state has no text: %s", *body)
	}
	for _, forbidden := range []string{"did:", "handle", "space"} {
		if strings.Contains(strings.ToLower(string(*body)), forbidden) {
			t.Fatalf("body contains %q: %s", forbidden, *body)
		}
	}
	// include_usage は送らない（11 §5.1）。
	if strings.Contains(string(*body), "include_usage") {
		t.Fatalf("body contains include_usage: %s", *body)
	}
}

// 429・5xx・400・401・タイムアウトはすべてエラーになり、呼び出し側が pending にする（11 §5.4）。
func TestJevErrors(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadRequest, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, _ := newJevServer(t, status, []byte(`{"error":"x"}`))
			jev := newJev(t, srv)

			if _, _, err := jev.Assess(context.Background(), service.Content{Kind: service.ContentKindInstancePost, Text: "x"}); err == nil {
				t.Fatalf("status %d: want error", status)
			}
			var apiErr *nanogpt.APIError
			if _, _, err := jev.Assess(context.Background(), service.Content{Kind: service.ContentKindInstancePost, Text: "x"}); !errors.As(err, &apiErr) || apiErr.StatusCode != status {
				t.Fatalf("status %d: error = %v, want *nanogpt.APIError", status, err)
			}
		})
	}

	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		jev := newJev(t, srv)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if _, _, err := jev.Assess(ctx, service.Content{Kind: service.ContentKindInstancePost, Text: "x"}); err == nil {
			t.Fatal("timeout: want error")
		}
	})
}
