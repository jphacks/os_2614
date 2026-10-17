package schedule

import (
	"encoding/json"
	"strings"
)

// GroupSchedule は 09 §3 の有効な時間帯。slots（曜日ごと）か dates（一回限り）の
// どちらかに当てはまれば有効。
type GroupSchedule struct {
	TZ    string              `json:"tz"`
	Slots []GroupScheduleSlot `json:"slots"`
	Dates []GroupScheduleDate `json:"dates"`
}

type GroupScheduleSlot struct {
	Weekday int    `json:"weekday"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

// GroupScheduleDate は日付を指定した一回限りの時間帯（09 §3）。自動グループが使う。
type GroupScheduleDate struct {
	Date  string `json:"date"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// Window は schedule の 1 つの時間帯。Date があれば一回限り、なければ Weekday（1=月〜7=日）。
type Window struct {
	Weekday int
	Date    string
	Start   int
	End     int
}

// SchedulesOverlap は 2 つの schedule の時間帯が重なるかを返す（09 §2）。
// schedule がない（常時有効）ものは何とでも重なる。
func SchedulesOverlap(a *GroupSchedule, bRaw *string) bool {
	aWindows, aAlways := ScheduleWindows(a)
	bWindows, bAlways := RawScheduleWindows(bRaw)
	if aAlways || bAlways {
		return true
	}
	for _, x := range aWindows {
		for _, y := range bWindows {
			if SameScheduleDay(x, y) && RangesOverlap(x.Start, x.End, y.Start, y.End) {
				return true
			}
		}
	}
	return false
}

// SameScheduleDay は 2 つの窓が同じ日（日付か曜日）に属するかを返す。
func SameScheduleDay(x, y Window) bool {
	switch {
	case x.Date != "" && y.Date != "":
		return x.Date == y.Date
	case x.Date != "":
		return WeekdayOf(x.Date) == y.Weekday
	case y.Date != "":
		return x.Weekday == WeekdayOf(y.Date)
	default:
		return x.Weekday == y.Weekday
	}
}

// ScheduleWindows は入力 schedule の窓を返す。指定がなければ (nil, true)（常時有効）。
func ScheduleWindows(a *GroupSchedule) ([]Window, bool) {
	if a == nil || (len(a.Slots) == 0 && len(a.Dates) == 0) {
		return nil, true
	}
	return WindowsOf(a), false
}

// RawScheduleWindows は保存済みの schedule_json の窓を返す。指定がなければ (nil, true)。
func RawScheduleWindows(raw *string) ([]Window, bool) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, true
	}
	var s GroupSchedule
	if err := json.Unmarshal([]byte(*raw), &s); err != nil || (len(s.Slots) == 0 && len(s.Dates) == 0) {
		return nil, true
	}
	return WindowsOf(&s), false
}

// WindowsOf は schedule の slots と dates を、時刻を分に直した窓の列にする。
func WindowsOf(s *GroupSchedule) []Window {
	out := make([]Window, 0, len(s.Slots)+len(s.Dates))
	for _, slot := range s.Slots {
		start, ok1 := ParseClock(slot.Start)
		end, ok2 := ParseClock(slot.End)
		if !ok1 || !ok2 {
			continue
		}
		out = append(out, Window{Weekday: slot.Weekday, Start: start, End: end})
	}
	for _, d := range s.Dates {
		start, ok1 := ParseClock(d.Start)
		end, ok2 := ParseClock(d.End)
		if !ok1 || !ok2 {
			continue
		}
		out = append(out, Window{Date: d.Date, Start: start, End: end})
	}
	return out
}
