package cli

import "testing"

func TestDashboardNoOpen(t *testing.T) {
	cases := []struct {
		noOpen, open bool
		wantNoOpen   bool
	}{
		{false, false, true},  // default: no flags → do NOT open (the fix)
		{false, true, false},  // --open → open
		{true, false, true},   // --no-open → no open
		{true, true, true},    // --no-open wins over --open
	}
	for _, c := range cases {
		if got := dashboardNoOpen(c.noOpen, c.open); got != c.wantNoOpen {
			t.Errorf("dashboardNoOpen(noOpen=%v, open=%v) = %v, want %v", c.noOpen, c.open, got, c.wantNoOpen)
		}
	}
}
