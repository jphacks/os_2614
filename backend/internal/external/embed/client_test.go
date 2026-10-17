package embed

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type capturedRequest struct {
	method      string
	contentType string
	body        []byte
}

func newCaptureServer(t *testing.T, status int, response string, captured *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.contentType = r.Header.Get("Content-Type")
		captured.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, response)
	}))
}

func decodeRequest(t *testing.T, body []byte) embedRequest {
	t.Helper()
	var req embedRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode request: %v (body=%s)", err, body)
	}
	return req
}

func TestEmbedSendsPromptAndNormalizes(t *testing.T) {
	var captured capturedRequest
	// dimensions が効かないサーバを想定し、4 次元を返す。
	srv := newCaptureServer(t, http.StatusOK,
		`{"data":[{"index":0,"embedding":[3,4,0,0]},{"index":1,"embedding":[1,2,3,4]}]}`, &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "embeddinggemma", 2)
	vecs, err := c.Embed(context.Background(), []string{"線形代数 II", "物理学"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if captured.method != http.MethodPost {
		t.Fatalf("method = %q, want POST", captured.method)
	}
	if captured.contentType != "application/json" {
		t.Fatalf("content-type = %q, want application/json", captured.contentType)
	}
	req := decodeRequest(t, captured.body)
	if req.Model != "embeddinggemma" {
		t.Fatalf("model = %q, want embeddinggemma", req.Model)
	}
	if req.Dimensions != 2 {
		t.Fatalf("dimensions = %d, want 2", req.Dimensions)
	}
	wantInput := []string{"task: clustering | query: 線形代数 II", "task: clustering | query: 物理学"}
	if !reflect.DeepEqual(req.Input, wantInput) {
		t.Fatalf("input = %#v, want %#v", req.Input, wantInput)
	}

	if len(vecs) != 2 {
		t.Fatalf("len(vecs) = %d, want 2", len(vecs))
	}
	// [3,4,0,0] → 正規化 [0.6,0.8,0,0] → 2 次元に切り詰め → 再正規化 [0.6,0.8]。
	assertVector(t, vecs[0], []float32{0.6, 0.8})
	// [1,2,3,4] → 正規化 → 先頭 2 次元 → 再正規化。
	assertVector(t, vecs[1], []float32{0.4472136, 0.8944272})
}

func TestEmbedTruncatesAndRenormalizes(t *testing.T) {
	var captured capturedRequest
	srv := newCaptureServer(t, http.StatusOK, `{"data":[{"index":0,"embedding":[1,0,0,0]}]}`, &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "m", 3)
	vecs, err := c.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs[0]) != 3 {
		t.Fatalf("len = %d, want 3", len(vecs[0]))
	}
	assertVector(t, vecs[0], []float32{1, 0, 0})
}

func TestEmbedNoTruncateWhenServerHonorsDimensions(t *testing.T) {
	var captured capturedRequest
	srv := newCaptureServer(t, http.StatusOK, `{"data":[{"index":0,"embedding":[0,5]}]}`, &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "m", 2)
	vecs, err := c.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	// 2 次元しか返らないので切り詰めは起きず、正規化だけ。
	assertVector(t, vecs[0], []float32{0, 1})
}

func TestEmbedEmptyInput(t *testing.T) {
	c := NewClient("http://127.0.0.1:1/v1/embeddings", "m", 2)
	vecs, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed(nil): %v", err)
	}
	if len(vecs) != 0 {
		t.Fatalf("len(vecs) = %d, want 0", len(vecs))
	}
}

func TestEmbedServerError(t *testing.T) {
	var captured capturedRequest
	srv := newCaptureServer(t, http.StatusInternalServerError, `{"error":"boom"}`, &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "m", 2)
	_, err := c.Embed(context.Background(), []string{"x"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", apiErr.StatusCode)
	}
}

func TestEmbedVectorCountMismatch(t *testing.T) {
	var captured capturedRequest
	srv := newCaptureServer(t, http.StatusOK, `{"data":[{"index":0,"embedding":[1,0]}]}`, &captured)
	defer srv.Close()

	c := NewClient(srv.URL, "m", 2)
	if _, err := c.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("Embed with mismatch: err = nil, want error")
	}
}

func assertVector(t *testing.T, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if math.Abs(float64(got[i]-want[i])) > 1e-5 {
			t.Fatalf("vec = %v, want %v", got, want)
		}
	}
	var sum float64
	for _, f := range got {
		sum += float64(f) * float64(f)
	}
	if math.Abs(math.Sqrt(sum)-1) > 1e-5 {
		t.Fatalf("vec = %v is not L2 normalized", got)
	}
}
