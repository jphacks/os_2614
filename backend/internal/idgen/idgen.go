// Package idgen は ID と乱数の生成を差し替え可能にする（15-testing.md §2）。
package idgen

import (
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

type Generator interface {
	// NewID は行の ID（UUIDv7）を返す。
	NewID() string
	// Bytes は暗号学的乱数を n バイト返す。
	Bytes(n int) []byte
}

type Real struct{}

func (Real) NewID() string {
	return uuid.Must(uuid.NewV7()).String()
}

func (Real) Bytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return b
}

// Seq はテスト用の、決まった列を返す生成器。
type Seq struct {
	mu sync.Mutex
	n  int
}

func (s *Seq) NewID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return fmt.Sprintf("00000000-0000-7000-8000-%012d", s.n)
}

func (s *Seq) Bytes(n int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(s.n + i)
	}
	return b
}
