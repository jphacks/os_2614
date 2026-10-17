package idgen

import (
	"sync"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/applemango/chain/internal/clock"
)

// TIDGenerator は record の rkey に使う TID（10 §2）を作る。
// テストで再現できるように差し替えられる。
type TIDGenerator interface {
	Next() syntax.TID
}

// TIDClock は clock.Clock の時刻から TID を作る。同じ時刻でも単調増加になるように補正する。
type TIDClock struct {
	clk     clock.Clock
	clockID uint
	mu      sync.Mutex
	last    int64
}

func NewTIDGenerator(clk clock.Clock, clockID uint) *TIDClock {
	return &TIDClock{clk: clk, clockID: clockID & 0x3FF}
}

func (g *TIDClock) Next() syntax.TID {
	micros := g.clk.Now().UTC().UnixMicro()
	g.mu.Lock()
	if micros <= g.last {
		micros = g.last + 1
	}
	g.last = micros
	g.mu.Unlock()
	return syntax.NewTID(micros, g.clockID)
}

// SeqTID はテスト用の、決まった列を返す TID 生成器。
type SeqTID struct {
	mu   sync.Mutex
	n    uint64
	base uint64
}

// NewSeqTID は base から始まる連番の TID を返す。base は TID の下位ビットに使う。
func NewSeqTID() *SeqTID { return &SeqTID{base: 1 << 40} }

func (s *SeqTID) Next() syntax.TID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return syntax.NewTIDFromInteger(s.base + s.n)
}
