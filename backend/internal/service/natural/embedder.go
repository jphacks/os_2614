package natural

import (
	"context"
	"time"
)

// Embedder は文の embedding を返す（09 §8.3）。返すベクトルは L2 正規化済み。
// 実装は internal/external/embed にある。CHAIN_EMBED_URL が空なら nil にして計算しない。
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
}

// 09 §8.3 の調整値。環境変数では上書きしない（02 §6）。
const (
	// EmbedTimeout は予定を保存するときに embedding の計算を待つ上限。過ぎても保存は成功させる。
	EmbedTimeout = 3 * time.Second
	// EmbedBackfillBatch は再計算のワーカーが 1 回に計算する件数。
	EmbedBackfillBatch = 50
)
