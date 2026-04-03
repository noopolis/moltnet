package store

import "testing"

func TestMessageWhereClause(t *testing.T) {
	t.Parallel()

	cases := []struct {
		scope messageScope
		want  string
	}{
		{scope: messageScopeRoom, want: `m.room_id = ? AND m.target_kind = 'room'`},
		{scope: messageScopeThread, want: `m.thread_id = ? AND m.target_kind = 'thread'`},
		{scope: messageScopeDM, want: `m.dm_id = ? AND m.target_kind = 'dm'`},
	}

	for _, test := range cases {
		got, err := messageWhereClause(test.scope, "m")
		if err != nil {
			t.Fatalf("messageWhereClause(%q) error = %v", test.scope, err)
		}
		if got != test.want {
			t.Fatalf("unexpected clause %q", got)
		}
	}

	if _, err := messageWhereClause(messageScope("wat"), "m"); err == nil {
		t.Fatal("expected unsupported scope error")
	}
}
