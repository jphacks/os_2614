// Package worker はプロセス内の常駐ワーカーを持つ（specification/backend/01-architecture.md §2）。
// 各ワーカーは Tick(ctx) を公開し、テストでは ticker を使わず直接呼ぶ（15-testing.md §2）。
// 業務ロジックは service に置き、ワーカーは呼び出しの周期と排他（lock:）だけを担う。
package worker
