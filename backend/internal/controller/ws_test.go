//go:build integration

package controller_test

import (
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/testutil"
)

// WebSocket の結合テスト（10 §7）。実際にサーバーを立て、2 つの接続で確かめる。

func startServer(t *testing.T, app *fiber.App) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	return ln.Addr().String()
}

func dialWS(t *testing.T, addr string) *websocket.Conn {
	t.Helper()
	url := "ws://" + addr + "/api/v1/ws"
	var conn *websocket.Conn
	var err error
	for i := 0; i < 50; i++ {
		conn, _, err = websocket.DefaultDialer.Dial(url, nil)
		if err == nil {
			t.Cleanup(func() { _ = conn.Close() })
			return conn
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("dial %s: %v", url, err)
	return nil
}

func sendWS(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_ = conn.SetWriteDeadline(clock.Real{}.Now().Add(2 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readUntil(t *testing.T, conn *websocket.Conn, timeout time.Duration, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := clock.Real{}.Now().Add(timeout)
	for {
		_ = conn.SetReadDeadline(deadline)
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if match(m) {
			return m
		}
	}
}

func connectAndAuth(t *testing.T, addr, token string) *websocket.Conn {
	t.Helper()
	conn := dialWS(t, addr)
	sendWS(t, conn, map[string]any{"type": "auth", "token": token})
	readUntil(t, conn, 3*time.Second, func(m map[string]any) bool { return m["type"] == "ready" })
	return conn
}

func TestWebSocketPostCreated(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.Consent(t, "did:plc:alice")
	alice := env.Login(t, "did:plc:alice")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	env.Consent(t, "did:plc:bob")
	bob := env.Login(t, "did:plc:bob")

	space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	group(t, env, "sp1", true)
	member(t, env, "sp1", "did:plc:alice", true)
	member(t, env, "sp1", "did:plc:bob", true)

	addr := startServer(t, app)
	connAlice := connectAndAuth(t, addr, alice)
	connBob := connectAndAuth(t, addr, bob)

	sendWS(t, connAlice, map[string]any{"type": "sub", "id": "a1", "space": "sp1"})
	readUntil(t, connAlice, 3*time.Second, func(m map[string]any) bool { return m["type"] == "subscribed" && m["id"] == "a1" })
	sendWS(t, connBob, map[string]any{"type": "sub", "id": "b1", "space": "sp1"})
	readUntil(t, connBob, 3*time.Second, func(m map[string]any) bool { return m["type"] == "subscribed" && m["id"] == "b1" })

	// alice が REST で投稿する。
	status, body := request(t, app, http.MethodPost, "/api/v1/posts", `{"spaceIds":["sp1"],"text":"hello ws"}`, alice)
	if status != http.StatusCreated {
		t.Fatalf("post: status = %d, want 201 (body=%s)", status, body)
	}

	ev := readUntil(t, connBob, 3*time.Second, func(m map[string]any) bool {
		return m["type"] == "event" && m["event"] == "post.created"
	})
	if ev["space"] != "sp1" {
		t.Fatalf("event space = %v, want sp1", ev["space"])
	}
	data, ok := ev["data"].(map[string]any)
	if !ok || data["uri"] == "" || data["text"] != "hello ws" {
		t.Fatalf("event data = %v, want the created post", ev["data"])
	}
}

func TestWebSocketAuthTimeout(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	addr := startServer(t, app)
	conn := dialWS(t, addr)

	// auth を送らないと、設定した短い時間で切断される（10 §7.1）。
	_ = conn.SetReadDeadline(clock.Real{}.Now().Add(5 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected the connection to be closed without auth")
	}
}

func TestWebSocketForbiddenSubscription(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	env.CreateUser(t, "did:plc:alice", "alice.test")
	alice := env.Login(t, "did:plc:alice")
	// alice は sp1 のメンバーだが sp-other のメンバーではない。
	space(t, env, "sp1", "jp.i32.chain.space.group", "active", "did:plc:alice")
	group(t, env, "sp1", true)
	member(t, env, "sp1", "did:plc:alice", true)
	space(t, env, "sp-other", "jp.i32.chain.space.group", "active", "did:plc:alice")
	group(t, env, "sp-other", true)

	addr := startServer(t, app)
	conn := connectAndAuth(t, addr, alice)

	sendWS(t, conn, map[string]any{"type": "sub", "id": "c1", "space": "sp-other"})
	ev := readUntil(t, conn, 3*time.Second, func(m map[string]any) bool { return m["type"] == "error" })
	if ev["code"] != "forbidden" {
		t.Fatalf("error code = %v, want forbidden", ev["code"])
	}
	if ev["id"] != "c1" {
		t.Fatalf("error id = %v, want c1", ev["id"])
	}
}
