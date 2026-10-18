package friend

import "testing"

// 07 §2.4: 相互フォロー ∧ ブロック無し ∧ 会った裏付け、がそろった組だけが friendships を持つ。
func TestDecide(t *testing.T) {
	tests := []struct {
		name       string
		in         reconcileInput
		wantAction reconcileAction
		wantVia    string
	}{
		{"nothing", reconcileInput{}, actionNone, ""},
		{"mutual only", reconcileInput{Mutual: true}, actionNone, ""},
		{"evidence only", reconcileInput{Encounter: true}, actionNone, ""},
		{"one-way follow with invite", reconcileInput{Invite: true}, actionNone, ""},
		{"mutual + encounter", reconcileInput{Mutual: true, Encounter: true}, actionAdd, ViaEncounter},
		{"mutual + space", reconcileInput{Mutual: true, Space: true}, actionAdd, ViaSpace},
		{"mutual + invite", reconcileInput{Mutual: true, Invite: true}, actionAdd, ViaInvite},
		{"invite wins over encounter and space", reconcileInput{Mutual: true, Invite: true, Encounter: true, Space: true}, actionAdd, ViaInvite},
		{"encounter wins over space", reconcileInput{Mutual: true, Encounter: true, Space: true}, actionAdd, ViaEncounter},
		{"blocked", reconcileInput{Mutual: true, Encounter: true, Blocked: true}, actionNone, ""},
		{"cap reached", reconcileInput{Mutual: true, Encounter: true, CapReached: true}, actionNone, ""},
		{"already a friend", reconcileInput{Mutual: true, Encounter: true, Existing: ViaEncounter}, actionNone, ""},
		{"friend keeps its first via when another evidence appears", reconcileInput{Mutual: true, Invite: true, Encounter: true, Existing: ViaEncounter}, actionNone, ""},
		{"unfollowed", reconcileInput{Encounter: true, Existing: ViaEncounter}, actionRemove, ""},
		{"blocked after being friends", reconcileInput{Mutual: true, Encounter: true, Blocked: true, Existing: ViaEncounter}, actionRemove, ""},
		{"encounter hidden", reconcileInput{Mutual: true, Existing: ViaEncounter}, actionRemove, ""},
		{"encounter hidden but space remains", reconcileInput{Mutual: true, Space: true, Existing: ViaEncounter}, actionNone, ""},
		{"cap does not remove existing", reconcileInput{Mutual: true, Encounter: true, CapReached: true, Existing: ViaEncounter}, actionNone, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action, via := decide(tc.in)
			if action != tc.wantAction || via != tc.wantVia {
				t.Fatalf("decide(%+v) = %v, %q; want %v, %q", tc.in, action, via, tc.wantAction, tc.wantVia)
			}
		})
	}
}
