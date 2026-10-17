package realtime

import (
	"context"
	"encoding/json"
	"sync"
)

const (
	// SendQueue は 1 接続あたりの送信キュー。超えた接続は切断する（10 §7.4）。
	SendQueue = 256
	// MaxSubscriptions は 1 接続あたりの購読 Space 数の上限（10 §7.2）。
	MaxSubscriptions = 50
)

// Event は配信するイベント（10 §7.3）。Space か User のどちらかを宛先に持つ。
type Event struct {
	Name  string
	Space string
	User  string
	Data  any
}

// Broker はイベントの配信口。service はこれ越しに publish する（10 §7.4）。
type Broker interface {
	Publish(ctx context.Context, e Event)
}

type wireEvent struct {
	Type  string `json:"type"`
	Event string `json:"event"`
	Space string `json:"space,omitempty"`
	Data  any    `json:"data,omitempty"`
}

// Client は 1 つの WebSocket 接続を表す。送信は非同期で、キューが溢れたら Send が false になる。
type Client struct {
	did       string
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once
	mu        sync.RWMutex
	spaces    map[string]struct{}
}

func NewClient(did string) *Client {
	return &Client{
		did:    did,
		send:   make(chan []byte, SendQueue),
		done:   make(chan struct{}),
		spaces: map[string]struct{}{},
	}
}

func (c *Client) DID() string { return c.did }

// Messages は書き込み用の送信キュー。
func (c *Client) Messages() <-chan []byte { return c.send }

// Done は接続が閉じたときに閉じる。
func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) Close() { c.closeOnce.Do(func() { close(c.done) }) }

// Send はキューに積む。溢れている、または閉じていれば false。
func (c *Client) Send(payload []byte) bool {
	select {
	case c.send <- payload:
		return true
	default:
		return false
	}
}

// Subscribe は購読を足す。上限を超えると false。
func (c *Client) Subscribe(space string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.spaces[space]; ok {
		return true
	}
	if len(c.spaces) >= MaxSubscriptions {
		return false
	}
	c.spaces[space] = struct{}{}
	return true
}

func (c *Client) Unsubscribe(space string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.spaces, space)
}

func (c *Client) Subscribed(space string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.spaces[space]
	return ok
}

func (c *Client) Subscriptions() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.spaces))
	for s := range c.spaces {
		out = append(out, s)
	}
	return out
}

// Hub はプロセス内の接続の集合（10 §7.4）。Broker を実装する。
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

func NewHub() *Hub { return &Hub{clients: map[*Client]struct{}{}} }

var _ Broker = (*Hub)(nil)

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.Close()
}

// ClientCount は現在の接続数。テストと監視のため。
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Publish は Space チャンネルとユーザーチャンネルの購読者へ配る。
// 送信が詰まった接続は切断する（10 §7.4）。
func (h *Hub) Publish(_ context.Context, e Event) {
	payload, err := json.Marshal(wireEvent{Type: "event", Event: e.Name, Space: e.Space, Data: e.Data})
	if err != nil {
		return
	}

	h.mu.RLock()
	targets := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		if e.Space != "" && c.Subscribed(e.Space) {
			targets = append(targets, c)
			continue
		}
		if e.User != "" && c.DID() == e.User {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range targets {
		if !c.Send(payload) {
			h.Unregister(c)
		}
	}
}
