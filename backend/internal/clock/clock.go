// Package clock は時刻の取得を差し替え可能にする。
// time.Now() はこのパッケージ以外で呼ばない（specification/backend/15-testing.md §2）。
package clock

import (
	"sync"
	"time"
)

// Ticker は一定間隔で発火するタイマー。実時間の間隔を使う処理（WebSocket の ping など）に使う。
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type Clock interface {
	Now() time.Time
	// NewTicker は d ごとに発火する Ticker を返す。実装は実時間でも Fake でもよい。
	NewTicker(d time.Duration) Ticker
}

type Real struct{}

func (Real) Now() time.Time { return time.Now() }

type realTicker struct{ t *time.Ticker }

func (t *realTicker) C() <-chan time.Time { return t.t.C }
func (t *realTicker) Stop()               { t.t.Stop() }

func (Real) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		d = time.Millisecond
	}
	return &realTicker{t: time.NewTicker(d)}
}

// Fake はテスト用の、手で進める時計。
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

func NewFake(t time.Time) *Fake { return &Fake{now: t} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance は時計を進め、その間に発火すべき Ticker をすべて発火させる（チャネルは容量 1 なので取りこぼす）。
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	now := f.now
	for _, tk := range f.tickers {
		tk.fire(now)
	}
}

func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

func (f *Fake) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		d = time.Millisecond
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	tk := &fakeTicker{ch: make(chan time.Time, 1), d: d, next: f.now.Add(d)}
	f.tickers = append(f.tickers, tk)
	return tk
}

type fakeTicker struct {
	mu   sync.Mutex
	ch   chan time.Time
	d    time.Duration
	next time.Time
	stop bool
}

// fire は now までの発火を試みる。チャネルは容量 1 なので、追いつけない分は捨てる。
func (t *fakeTicker) fire(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stop {
		return
	}
	for !t.next.After(now) {
		select {
		case t.ch <- t.next:
		default:
		}
		t.next = t.next.Add(t.d)
	}
}

func (t *fakeTicker) C() <-chan time.Time { return t.ch }

func (t *fakeTicker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stop = true
}

// UnixMilli は DB に保存する時刻の形式（04 §1）。
func UnixMilli(t time.Time) int64 { return t.UnixMilli() }

func FromUnixMilli(ms int64) time.Time { return time.UnixMilli(ms).UTC() }
