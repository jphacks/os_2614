package nanogpt

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testText = "明日の集合は 9:00 正門。遅れる人は連絡して"

// sampleRequest builds the §5.2 request. Model is deliberately wrong so the test
// proves Decide overwrites it with c.Model. The caller puts no DID/handle anywhere.
func sampleRequest() Request {
	return Request{
		Model: "this-must-be-overridden",
		State: State{Kind: "instance_post", Text: testText},
		Questions: map[string]Question{
			"safety": {
				Type:         "choice",
				Instructions: "SNS の投稿として、この文章をどう扱うべきか。",
				Criteria: map[string]*string{
					"ok":    strptr("問題のない内容"),
					"warn":  strptr("攻撃的・性的・暴力的・センシティブだが、特定の個人や集団への加害ではない内容"),
					"block": strptr("特定の個人や集団への誹謗中傷・脅迫・差別、個人情報の暴露、自傷や犯罪の扇動"),
				},
			},
			"category": {
				Type:         "choice",
				Instructions: "最も当てはまる問題の種類",
				Criteria: map[string]*string{
					"none": nil, "harassment": nil, "hate": nil, "sexual": nil,
					"violence": nil, "self_harm": nil, "personal_info": nil, "spam": nil,
				},
			},
			"pin": {
				Type:         "noul",
				Instructions: "グループのメンバーが後で見返す必要のある情報を含むか",
			},
		},
	}
}

func strptr(s string) *string { return &s }

func requireMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want JSON object, got %T", v)
	}
	return m
}

func TestDecide_DecodesResponse_whenServerReturns200(t *testing.T) {
	// given
	const responseBody = `{
	  "answers": {
	    "safety":   { "type": "choice", "choice": "ok", "confidence": 0.97, "probabilities": { "ok": 0.97, "warn": 0.02, "block": 0.01 } },
	    "category": { "type": "choice", "choice": "none", "confidence": 0.95, "probabilities": { "none": 0.95 } },
	    "pin":      { "type": "noul", "noul": 0.88 }
	  }
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, responseBody)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "test-key", "typesafe/jev-1.13")

	// when
	resp, err := c.Decide(context.Background(), sampleRequest())

	// then
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	safety, ok := resp.Answers["safety"]
	if !ok {
		t.Fatalf("answers missing safety: %+v", resp.Answers)
	}
	if safety.Type != "choice" || safety.Choice != "ok" {
		t.Errorf("safety = %+v, want type=choice choice=ok", safety)
	}
	if safety.Confidence != 0.97 {
		t.Errorf("safety.Confidence = %v, want 0.97", safety.Confidence)
	}
	if safety.Probabilities["block"] != 0.01 {
		t.Errorf("safety.Probabilities[block] = %v, want 0.01", safety.Probabilities["block"])
	}
	pin, ok := resp.Answers["pin"]
	if !ok {
		t.Fatalf("answers missing pin: %+v", resp.Answers)
	}
	if pin.Type != "noul" || pin.Noul != 0.88 {
		t.Errorf("pin = %+v, want type=noul noul=0.88", pin)
	}
}

func TestDecide_SendsExpectedBody_whenCalled(t *testing.T) {
	// given
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"answers":{}}`)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "test-key", "typesafe/jev-1.13")

	// when
	if _, err := c.Decide(context.Background(), sampleRequest()); err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	// then
	raw := string(got)
	if strings.Contains(raw, "did:") {
		t.Errorf("request body leaked a DID: %s", raw)
	}

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("request body is not valid JSON: %v (%s)", err, raw)
	}

	if decoded["model"] != "typesafe/jev-1.13" {
		t.Errorf("model = %v, want typesafe/jev-1.13 (Decide must overwrite req.Model)", decoded["model"])
	}

	state := requireMap(t, decoded["state"])
	if state["kind"] != "instance_post" {
		t.Errorf("state.kind = %v, want instance_post", state["kind"])
	}
	if state["text"] != testText {
		t.Errorf("state.text = %v, want %q", state["text"], testText)
	}
	for _, absent := range []string{"did", "handle", "space", "user", "session_id"} {
		if _, ok := state[absent]; ok {
			t.Errorf("state must not contain %q", absent)
		}
	}

	questions := requireMap(t, decoded["questions"])
	for _, name := range []string{"safety", "category", "pin"} {
		if _, ok := questions[name]; !ok {
			t.Errorf("questions missing %q", name)
		}
	}

	category := requireMap(t, questions["category"])
	criteria := requireMap(t, category["criteria"])
	if v, ok := criteria["none"]; !ok || v != nil {
		t.Errorf("category.criteria[none] = %v (present=%v), want JSON null", v, ok)
	}
	if len(criteria) != 8 {
		t.Errorf("category.criteria has %d entries, want 8", len(criteria))
	}

	pin := requireMap(t, questions["pin"])
	if _, ok := pin["criteria"]; ok {
		t.Errorf("pin question must omit criteria, got %v", pin["criteria"])
	}
	if pin["type"] != "noul" {
		t.Errorf("pin.type = %v, want noul", pin["type"])
	}
}

func TestDecide_SendsAuthorizationHeader_whenCalled(t *testing.T) {
	// given
	var auth, contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		contentType = r.Header.Get("Content-Type")
		_, _ = io.WriteString(w, `{"answers":{}}`)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "secret-key", "typesafe/jev-1.13")

	// when
	if _, err := c.Decide(context.Background(), sampleRequest()); err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}

	// then
	if auth != "Bearer secret-key" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer secret-key")
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
}

func TestDecide_ReturnsAPIError_whenStatusNotOK(t *testing.T) {
	for _, status := range []int{400, 401, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"server said no"}`)
			}))
			defer srv.Close()
			c := NewClient(srv.URL, "test-key", "typesafe/jev-1.13")

			// when
			_, err := c.Decide(context.Background(), sampleRequest())

			// then
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("want *APIError, got %T: %v", err, err)
			}
			if apiErr.StatusCode != status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, status)
			}
			if !strings.Contains(apiErr.Body, "server said no") {
				t.Errorf("Body = %q, want it to contain the server message", apiErr.Body)
			}
		})
	}
}

func TestDecide_ReturnsContextError_whenDeadlineExceeded(t *testing.T) {
	// given: a handler that never responds until the test releases it.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)
	c := NewClient(srv.URL, "test-key", "typesafe/jev-1.13")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	// when
	_, err := c.Decide(ctx, sampleRequest())

	// then
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("context error must not be *APIError, got %v", apiErr)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want context.DeadlineExceeded, got %v", err)
	}
}
