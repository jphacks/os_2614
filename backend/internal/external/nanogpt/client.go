package nanogpt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// userAgent は NanoGPT へ送る固定的な User-Agent。
const userAgent = "chain-backend"

// maxErrorBody は非 200 応答から保持する本文の上限（4 KiB）。
const maxErrorBody = 4 << 10

// maxResponseBody は 200 応答本文の上限。API の本文上限は 1 MiB（11 §5.1）なのでそれに合わせる。
const maxResponseBody = 1 << 20

// State は Decisions API に送る "state"（11 §5.2）。
// 本文とその種類だけを入れる。DID・handle・Space 名は決して入れない。
type State struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// Question は "questions" オブジェクトの 1 エントリ。
type Question struct {
	Type         string             `json:"type"`
	Instructions string             `json:"instructions"`
	Criteria     map[string]*string `json:"criteria,omitempty"`
}

// Request は Decisions API のリクエスト本文。
type Request struct {
	Model     string              `json:"model"`
	State     State               `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// Answer は "answers" オブジェクトの 1 エントリ。
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
}

// Response は Decisions API の応答本文。
type Response struct {
	Answers map[string]Answer `json:"answers"`
}

// APIError は非 200 応答に対して返る。
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("nanogpt: status %d: %s", e.StatusCode, e.Body)
}

// Client は POST {Endpoint} を呼ぶ。
type Client struct {
	Endpoint string       // 完全な URL。例: https://api.nano-gpt.com/api/v1/decisions
	APIKey   string       // "Authorization: Bearer <APIKey>" として送る
	Model    string       // 例: typesafe/jev-1.13
	HTTP     *http.Client // 注入可能。Timeout は設定しない（呼び出し側の context が制御する）
}

// NewClient は Client を返す。HTTP が nil のとき Decide は http.DefaultClient を使う。
func NewClient(endpoint, apiKey, model string) *Client {
	return &Client{Endpoint: endpoint, APIKey: apiKey, Model: model}
}

// Decide は req を送信して応答をデコードする。req.Model は c.Model で上書きする。
func (c *Client) Decide(ctx context.Context, req Request) (*Response, error) {
	req.Model = c.Model

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
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

	var out Response
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
