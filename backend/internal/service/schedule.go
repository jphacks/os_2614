package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// group schedule（09 §3）。schedule_json が NULL なら常時有効。
// slot は曜日（1=月〜7=日）と開始・終了時刻。end < start は日をまたぐ。

type scheduleSlot struct {
	Weekday int    `json:"weekday"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

type scheduleDef struct {
	TZ    string         `json:"tz"`
	Slots []scheduleSlot `json:"slots"`
}

// scheduleActive は now が schedule の有効時間帯に入るかを返す。
// 指定がなければ常時有効。壊れた値も常時有効として扱い、誤って全員を締め出さないようにする。
func scheduleActive(raw *string, now time.Time) bool {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return true
	}
	var def scheduleDef
	if err := json.Unmarshal([]byte(*raw), &def); err != nil || len(def.Slots) == 0 {
		return true
	}
	loc := time.UTC
	if def.TZ != "" {
		if l, err := time.LoadLocation(def.TZ); err == nil {
			loc = l
		}
	}
	t := now.In(loc)
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
		start, ok1 := parseClock(slot.Start)
		end, ok2 := parseClock(slot.End)
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
	return false
}

func parseClock(s string) (int, bool) {
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
