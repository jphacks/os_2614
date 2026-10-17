package fcm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/applemango/chain/internal/clock"
)

// capture はハンドラ（別 goroutine）とテスト本体の競合を避けるための記録箱。
type capture struct {
	mu         sync.Mutex
	tokenCalls int
	grantType  string
	assertion  string
	authHeader string
	method     string
	path       string
	body       sendRequest
}

func genKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return key
}

// newServer はトークンエンドポイントと送信エンドポイントを 1 つに載せたテストサーバーを返す。
func newServer(t *testing.T, cap *capture, sendStatus int, sendBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cap.mu.Lock()
		cap.tokenCalls++
		cap.grantType = r.PostFormValue("grant_type")
		cap.assertion = r.PostFormValue("assertion")
		cap.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"tok","expires_in":3600}`)
	})
	mux.HandleFunc("/v1/projects/proj/messages:send", func(w http.ResponseWriter, r *http.Request) {
		var body sendRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		cap.mu.Lock()
		cap.authHeader = r.Header.Get("Authorization")
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.body = body
		cap.mu.Unlock()
		w.WriteHeader(sendStatus)
		if sendBody != "" {
			_, _ = io.WriteString(w, sendBody)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	return &Client{
		ProjectID:  "proj",
		Email:      "svc@example.iam.gserviceaccount.com",
		PrivateKey: genKey(t),
		TokenURL:   srv.URL + "/token",
		BaseURL:    srv.URL,
		HTTP:       srv.Client(),
		Clock:      clock.Real{},
	}
}

func testMessage() Message {
	return Message{
		Token:       "device-token-secret",
		Title:       "Bob さんからフレンド申請",
		Body:        "承認しますか？",
		Data:        map[string]string{"type": "friend.request", "id": "42"},
		ChannelID:   "social",
		CollapseKey: "friend.request",
	}
}

// Given: token と message の両エンドポイントが成功する。
// When: Send を呼ぶ。
// Then: nil を返し、各リクエストが仕様どおりの形で送られる。
func TestClient_Send_when_happy_path(t *testing.T) {
	var cap capture
	srv := newServer(t, &cap, http.StatusOK, `{"name":"projects/proj/messages/1"}`)
	c := newTestClient(t, srv)

	if err := c.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send() = %v, want nil", err)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()

	if cap.tokenCalls != 1 {
		t.Errorf("token endpoint calls = %d, want 1", cap.tokenCalls)
	}
	if cap.grantType != jwtBearerGrant {
		t.Errorf("grant_type = %q, want %q", cap.grantType, jwtBearerGrant)
	}
	if cap.assertion == "" {
		t.Error("assertion empty, want signed JWT")
	}
	if cap.method != http.MethodPost {
		t.Errorf("method = %q, want POST", cap.method)
	}
	if cap.path != "/v1/projects/proj/messages:send" {
		t.Errorf("path = %q, want /v1/projects/proj/messages:send", cap.path)
	}
	if cap.authHeader != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", cap.authHeader, "Bearer tok")
	}

	m := cap.body.Message
	if m.Token != "device-token-secret" {
		t.Errorf("token = %q, want device token", m.Token)
	}
	if m.Notification == nil {
		t.Fatal("notification missing")
	}
	if m.Notification.Title != "Bob さんからフレンド申請" {
		t.Errorf("notification.title = %q", m.Notification.Title)
	}
	if m.Notification.Body != "承認しますか？" {
		t.Errorf("notification.body = %q", m.Notification.Body)
	}
	if m.Data["type"] != "friend.request" || m.Data["id"] != "42" {
		t.Errorf("data = %v", m.Data)
	}
	if m.Android == nil {
		t.Fatal("android config missing")
	}
	if m.Android.CollapseKey != "friend.request" {
		t.Errorf("android.collapse_key = %q", m.Android.CollapseKey)
	}
	if m.Android.Notification == nil {
		t.Fatal("android.notification missing")
	}
	if m.Android.Notification.ChannelID != "social" {
		t.Errorf("android.notification.channel_id = %q", m.Android.Notification.ChannelID)
	}
}

// Given: 空の ChannelID/CollapseKey/Data。
// When: Send を呼ぶ。
// Then: android と data がリクエストから落ちる。
func TestClient_Send_when_optional_empty_omits_fields(t *testing.T) {
	var cap capture
	srv := newServer(t, &cap, http.StatusOK, "")
	c := newTestClient(t, srv)

	if err := c.Send(context.Background(), Message{Token: "t", Title: "a", Body: "b"}); err != nil {
		t.Fatalf("Send() = %v, want nil", err)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.body.Message.Android != nil {
		t.Errorf("android = %+v, want nil", cap.body.Message.Android)
	}
	if cap.body.Message.Data != nil {
		t.Errorf("data = %v, want nil", cap.body.Message.Data)
	}
}

// Given: 一度 Send が成功してトークンがキャッシュ済み。
// When: もう一度 Send を呼ぶ。
// Then: トークンエンドポイントは 1 回しか叩かれない。
func TestClient_Send_when_cached_reuses_token(t *testing.T) {
	var cap capture
	srv := newServer(t, &cap, http.StatusOK, "")
	c := newTestClient(t, srv)

	for i := 0; i < 2; i++ {
		if err := c.Send(context.Background(), testMessage()); err != nil {
			t.Fatalf("Send() #%d = %v, want nil", i+1, err)
		}
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.tokenCalls != 1 {
		t.Errorf("token endpoint calls = %d, want 1 (cached)", cap.tokenCalls)
	}
}

// Given: message エンドポイントが 404 を返す。
// When: Send を呼ぶ。
// Then: errors.Is(err, ErrUnregistered) が真。
func TestClient_Send_when_unregistered(t *testing.T) {
	var cap capture
	body := `{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`
	srv := newServer(t, &cap, http.StatusNotFound, body)
	c := newTestClient(t, srv)

	err := c.Send(context.Background(), testMessage())
	if err == nil {
		t.Fatal("Send() = nil, want error")
	}
	if !errors.Is(err, ErrUnregistered) {
		t.Errorf("errors.Is(err, ErrUnregistered) = false, err = %v", err)
	}
	if strings.Contains(err.Error(), testMessage().Token) {
		t.Errorf("error leaks device token: %v", err)
	}
}

// Given: message エンドポイントが 500 を返す。
// When: Send を呼ぶ。
// Then: 非 nil かつ ErrUnregistered ではないエラー。
func TestClient_Send_when_server_error(t *testing.T) {
	var cap capture
	srv := newServer(t, &cap, http.StatusInternalServerError, `{"error":{"status":"INTERNAL"}}`)
	c := newTestClient(t, srv)

	err := c.Send(context.Background(), testMessage())
	if err == nil {
		t.Fatal("Send() = nil, want error")
	}
	if errors.Is(err, ErrUnregistered) {
		t.Errorf("errors.Is(err, ErrUnregistered) = true, want false")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want status code present", err)
	}
	if strings.Contains(err.Error(), testMessage().Token) {
		t.Errorf("error leaks device token: %v", err)
	}
}

// Given: LogSender。
// When: トークンを含むメッセージを送る。
// Then: nil を返す（送信せずログのみ）。
func TestLogSender_Send_smoke(t *testing.T) {
	if err := (LogSender{}).Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("LogSender.Send() = %v, want nil", err)
	}
}

// Given: サービスアカウント JSON を一時ファイルに置く。
// When: NewClient を呼ぶ。
// Then: 既定の URL と HTTP クライアントを持つクライアントを返す。
func TestNewClient_when_valid_credentials(t *testing.T) {
	key := genKey(t)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	sa := map[string]string{
		"project_id":   "proj",
		"client_email": "svc@example.iam.gserviceaccount.com",
		"private_key":  string(pemBytes),
	}
	raw, err := json.Marshal(sa)
	if err != nil {
		t.Fatalf("marshal service account: %v", err)
	}
	path := filepath.Join(t.TempDir(), "sa.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}

	c, err := NewClient(context.Background(), path, clock.Real{})
	if err != nil {
		t.Fatalf("NewClient() = %v, want nil", err)
	}
	if c.ProjectID != "proj" || c.Email != "svc@example.iam.gserviceaccount.com" {
		t.Errorf("client = %+v", c)
	}
	if c.TokenURL != defaultTokenURL || c.BaseURL != defaultBaseURL {
		t.Errorf("defaults not applied: token=%q base=%q", c.TokenURL, c.BaseURL)
	}
	if c.PrivateKey == nil || c.HTTP == nil || c.Clock == nil {
		t.Error("client fields not populated")
	}
}

// Given: 存在しない credPath。
// When: NewClient を呼ぶ。
// Then: エラーを返す。
func TestNewClient_when_file_missing(t *testing.T) {
	if _, err := NewClient(context.Background(), filepath.Join(t.TempDir(), "nope.json"), clock.Real{}); err == nil {
		t.Fatal("NewClient() = nil, want error")
	}
}
