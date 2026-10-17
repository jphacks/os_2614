// Package realtime は WebSocket の hub と配信を持つ（specification/backend/10-instance.md §7）。
// service は Broker interface 越しに publish し、Fiber や接続の型に依存しない。
package realtime
