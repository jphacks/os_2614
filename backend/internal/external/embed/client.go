// Package embed は OpenAI 互換の /v1/embeddings を呼ぶクライアントを持つ（09 §8.3）。
// 推論サーバ（Ollama、llama.cpp server、Text Embeddings Inference など）を backend と同じマシンで動かす想定。
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
)

const (
	// userAgent は推論サーバへ送る固定的な User-Agent。
	userAgent = "chain-backend"
	// taskPrefix は EmbeddingGemma の推奨プロンプト（09 §8.3）。モデルを替えたら見直す。
	taskPrefix = "task: clustering | query: "
	// maxErrorBody は非 200 応答から保持する本文の上限（4 KiB）。
	maxErrorBody = 4 << 10
	// maxResponseBody は 200 応答本文の上限。50 件 × 256 次元でも収まる大きさ。
	maxResponseBody = 4 << 20
)

// Client は POST {Endpoint} を呼ぶ。
type Client struct {
	Endpoint  string       // /v1/embeddings の完全な URL
	ModelName string       // 例: embeddinggemma
	Dim       int          // dimensions に送る次元数
	HTTP      *http.Client // 注入可能。nil なら http.DefaultClient（Timeout は呼び出し側の context が制御する）
}

// NewClient は Client を返す。
func NewClient(endpoint, model string, dim int) *Client {
	return &Client{Endpoint: endpoint, ModelName: model, Dim: dim}
}

// Model は使うモデル名を返す（service.Embedder）。
func (c *Client) Model() string { return c.ModelName }

// embedRequest は /v1/embeddings のリクエスト本文。
type embedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions"`
}

// embedResponse は /v1/embeddings の応答本文。
type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// APIError は非 200 応答に対して返る。
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("embed: status %d: %s", e.StatusCode, e.Body)
}

// Embed は texts の embedding を返す。各 text は `task: clustering | query: {text}` の形にして送る（09 §8.3）。
// 返るベクトルは L2 正規化し、Dim が効かないサーバ向けに先頭 Dim 次元に切り詰めてから再正規化する。
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	input := make([]string, len(texts))
	for i, t := range texts {
		input[i] = taskPrefix + t
	}
	body, err := json.Marshal(embedRequest{Model: c.ModelName, Input: input, Dimensions: c.Dim})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", userAgent)

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(errBody)}
	}

	var out embedResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embed: got %d vectors for %d inputs", len(out.Data), len(texts))
	}

	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(texts) {
			return nil, fmt.Errorf("embed: index %d out of range", d.Index)
		}
		vecs[d.Index] = normalizeAndTruncate(d.Embedding, c.Dim)
	}
	for i, v := range vecs {
		if v == nil {
			return nil, fmt.Errorf("embed: vector %d is missing", i)
		}
	}
	return vecs, nil
}

// normalizeAndTruncate は L2 正規化し、dim > 0 なら先頭 dim 次元に切り詰めてから再正規化する（09 §8.3）。
func normalizeAndTruncate(v []float32, dim int) []float32 {
	out := l2Normalize(v)
	if dim > 0 && len(out) > dim {
		out = l2Normalize(out[:dim])
	}
	return out
}

// l2Normalize は L2 ノルムで割る。ゼロベクトルはそのまま返す。
func l2Normalize(v []float32) []float32 {
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	out := make([]float32, len(v))
	if sum == 0 {
		copy(out, v)
		return out
	}
	norm := math.Sqrt(sum)
	for i, f := range v {
		out[i] = float32(float64(f) / norm)
	}
	return out
}
