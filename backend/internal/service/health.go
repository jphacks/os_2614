package service

import (
	"context"
)

type HealthService struct {
	Deps
}

type HealthStatus struct {
	SQLite bool `json:"sqlite"`
	Redis  bool `json:"redis"`
}

func (s *HealthService) Check(ctx context.Context) HealthStatus {
	return HealthStatus{
		SQLite: s.DB.PingContext(ctx) == nil,
		Redis:  s.Redis.Ping(ctx).Err() == nil,
	}
}

func (h HealthStatus) OK() bool { return h.SQLite && h.Redis }
