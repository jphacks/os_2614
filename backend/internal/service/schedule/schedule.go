package schedule

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// group schedule（09 §3）。schedule_json が NULL なら常時有効。
// slot は曜日（1=月〜7=日）と開始・終了時刻。date は一回限りの日付。end < start は日をまたぐ。
// slots と dates のどちらかに当てはまれば有効。

type scheduleSlot struct {
	Weekday int    `json:"weekday"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

type scheduleDef struct {
	TZ    string              `json:"tz"`
	Slots []scheduleSlot      `json:"slots"`
	Dates []GroupScheduleDate `json:"dates"`
}

// ScheduleActive は now が schedule の有効時間帯に入るかを返す。
// 指定がなければ常時有効。壊れた値も常時有効として扱い、誤って全員を締め出さないようにする。
func ScheduleActive(raw *string, now time.Time) bool {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return true
	}
	var def scheduleDef
	if err := json.Unmarshal([]byte(*raw), &def); err != nil {
		return true
	}
	if len(def.Slots) == 0 && len(def.Dates) == 0 {
		return true
	}
	t := now.In(ScheduleLocation(def.TZ))
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	minutes := t.Hour()*60 + t.Minute()
	prev := weekday - 1
	if prev == 0 {
		prev = 7
	}

	for _, slot := range def.Slots {
		start, ok1 := ParseClock(slot.Start)
		end, ok2 := ParseClock(slot.End)
		if !ok1 || !ok2 {
			continue
		}
		if start <= end {
			if slot.Weekday == weekday && minutes >= start && minutes < end {
				return true
			}
			continue
		}
		if slot.Weekday == weekday && minutes >= start {
			return true
		}
		if slot.Weekday == prev && minutes < end {
			return true
		}
	}

	date := t.Format(DateLayout)
	prevDate := t.AddDate(0, 0, -1).Format(DateLayout)
	for _, d := range def.Dates {
		start, ok1 := ParseClock(d.Start)
		end, ok2 := ParseClock(d.End)
		if !ok1 || !ok2 {
			continue
		}
		if start <= end {
			if d.Date == date && minutes >= start && minutes < end {
				return true
			}
			continue
		}
		if d.Date == date && minutes >= start {
			return true
		}
		if d.Date == prevDate && minutes < end {
			return true
		}
	}
	return false
}

const DateLayout = "2006-01-02"

// ScheduleLocation は tz の場所を返す。空・不正なら UTC。
func ScheduleLocation(tz string) *time.Location {
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			return l
		}
	}
	return time.UTC
}

// ScheduleOverlapsRange は date の [startMin, endMin) が schedule の時間帯と重なるかを返す。
// schedule の指定がなければ常時有効として true。
func ScheduleOverlapsRange(raw *string, date string, startMin, endMin int) bool {
	windows, always := RawScheduleWindows(raw)
	if always {
		return true
	}
	weekday := WeekdayOf(date)
	for _, w := range windows {
		switch {
		case w.Date != "":
			if w.Date != date {
				continue
			}
		case weekday != 0 && w.Weekday == weekday:
		default:
			continue
		}
		if RangesOverlap(w.Start, w.End, startMin, endMin) {
			return true
		}
	}
	return false
}

// ScheduleLatestEnd は schedule の時間帯のうち最も遅い終了時刻を返す（09 §8.5 の終了判定）。
// dates（一回限り）だけを対象にし、ないときは ok=false。
func ScheduleLatestEnd(raw *string, now time.Time) (time.Time, bool) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return time.Time{}, false
	}
	var def scheduleDef
	if err := json.Unmarshal([]byte(*raw), &def); err != nil {
		return time.Time{}, false
	}
	loc := ScheduleLocation(def.TZ)
	var latest time.Time
	found := false
	for _, d := range def.Dates {
		start, ok1 := ParseClock(d.Start)
		end, ok2 := ParseClock(d.End)
		if !ok1 || !ok2 {
			continue
		}
		day, err := time.ParseInLocation(DateLayout, d.Date, loc)
		if err != nil {
			continue
		}
		endAt := day.Add(time.Duration(end) * time.Minute)
		if start > end {
			endAt = endAt.Add(24 * time.Hour)
		}
		if !found || endAt.After(latest) {
			latest = endAt
			found = true
		}
	}
	return latest, found
}

// WeekdayOf は YYYY-MM-DD の曜日（1=月〜7=日）を返す。不正なら 0。
func WeekdayOf(date string) int {
	t, err := time.Parse(DateLayout, date)
	if err != nil {
		return 0
	}
	w := int(t.Weekday())
	if w == 0 {
		w = 7
	}
	return w
}

// RangesOverlap は 2 つの分の範囲 [start, end) が重なるかを返す。
func RangesOverlap(aStart, aEnd, bStart, bEnd int) bool {
	return aStart < bEnd && bStart < aEnd
}

func ParseClock(s string) (int, bool) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 24 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}
