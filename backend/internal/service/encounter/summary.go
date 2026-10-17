package encounter

import (
	"context"

	encountermodel "github.com/applemango/chain/internal/model/encounter"
	"github.com/applemango/chain/internal/service/core"
)

// EncounterSummary は GET /encounters/summary の値（06 §6、13 §3）。
type EncounterSummary struct {
	Today        int `json:"today"`
	Week         int `json:"week"`
	Month        int `json:"month"`
	Total        int `json:"total"`
	UniquePeople int `json:"uniquePeople"`
}

// Summary は did のすれ違い回数を数える（06 §6）。revealed に加えて pending / hidden も数え、
// 成立から core.SummaryHoldback 以内のものは数えない。区切りは Asia/Tokyo（週は月曜始まり）。
func (s *EncounterService) Summary(ctx context.Context, did string) (*EncounterSummary, error) {
	now := s.Clock.Now()
	todayStart, _ := core.JstDayBounds(now)
	c, err := encountermodel.CountEncounterSummary(ctx, s.DB, did,
		todayStart, core.JstWeekStart(now), core.JstMonthStart(now), now.Add(-core.SummaryHoldback))
	if err != nil {
		return nil, err
	}
	return &EncounterSummary{
		Today:        c.Today,
		Week:         c.Week,
		Month:        c.Month,
		Total:        c.Total,
		UniquePeople: c.UniquePeople,
	}, nil
}
