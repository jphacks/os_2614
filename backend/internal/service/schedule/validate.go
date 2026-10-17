package schedule

import (
	"strconv"
	"time"
)

// ValidateGroupSchedule は 09 §3 の規則を確かめる。
func ValidateGroupSchedule(s *GroupSchedule) map[string]string {
	fields := map[string]string{}
	if s == nil {
		return fields
	}
	if len(s.Slots) == 0 && len(s.Dates) == 0 {
		fields["schedule.slots"] = "1 件以上必要です"
	}
	if len(s.Slots) > 14 {
		fields["schedule.slots"] = "14 件までです"
	}
	if len(s.Dates) > 14 {
		fields["schedule.dates"] = "14 件までです"
	}
	if s.TZ == "" {
		fields["schedule.tz"] = "IANA のタイムゾーン名が必要です"
	} else if _, err := time.LoadLocation(s.TZ); err != nil {
		fields["schedule.tz"] = "IANA のタイムゾーン名で指定してください"
	}
	for i, slot := range s.Slots {
		key := "schedule.slots." + strconv.Itoa(i)
		if slot.Weekday < 1 || slot.Weekday > 7 {
			fields[key+".weekday"] = "1〜7 で指定してください"
		}
		start, ok1 := ParseClock(slot.Start)
		end, ok2 := ParseClock(slot.End)
		if !ok1 {
			fields[key+".start"] = "HH:MM で指定してください"
		}
		if !ok2 {
			fields[key+".end"] = "HH:MM で指定してください"
		}
		if ok1 && ok2 && start >= end {
			fields[key+".start"] = "開始は終了より前にしてください"
		}
	}
	for i, d := range s.Dates {
		key := "schedule.dates." + strconv.Itoa(i)
		if !ValidScheduleDate(d.Date) {
			fields[key+".date"] = "YYYY-MM-DD で指定してください"
		}
		start, ok1 := ParseClock(d.Start)
		end, ok2 := ParseClock(d.End)
		if !ok1 {
			fields[key+".start"] = "HH:MM で指定してください"
		}
		if !ok2 {
			fields[key+".end"] = "HH:MM で指定してください"
		}
		if ok1 && ok2 && start >= end {
			fields[key+".start"] = "開始は終了より前にしてください"
		}
	}
	return fields
}

// ValidScheduleDate は YYYY-MM-DD の実在する日付かを返す。
func ValidScheduleDate(date string) bool {
	t, err := time.Parse(DateLayout, date)
	return err == nil && t.Format(DateLayout) == date
}
