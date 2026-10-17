package service

import "testing"

func TestNotificationCollapseKeyPerSpace(t *testing.T) {
	one := notificationCollapseKey(NotificationGroupNearby, "space-1")
	two := notificationCollapseKey(NotificationGroupNearby, "space-2")
	if one != "group.nearby:space-1" {
		t.Fatalf("collapse_key = %q, want group.nearby:space-1", one)
	}
	if one == two {
		t.Fatalf("group.nearby の collapse_key が Space ごとになっていない: %q", one)
	}
	if got := notificationCollapseKey("encounter.daily", ""); got != "encounter.daily" {
		t.Fatalf("ダイジェストの collapse_key = %q, want encounter.daily", got)
	}
}
