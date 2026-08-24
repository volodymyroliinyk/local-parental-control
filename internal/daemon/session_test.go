package daemon

import "testing"

func TestSessionIDValidation(t *testing.T) {
	for _, valid := range []string{"2", "c1", "session-1", "session_1"} {
		if !sessionIDPattern.MatchString(valid) {
			t.Fatalf("valid session ID %q was rejected", valid)
		}
	}
	for _, invalid := range []string{"", "two words", "../2", "2;shutdown"} {
		if sessionIDPattern.MatchString(invalid) {
			t.Fatalf("invalid session ID %q was accepted", invalid)
		}
	}
}

func TestParseSessionState(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		unlocked  bool
		graphical bool
		wantErr   bool
	}{
		{name: "unlocked Wayland", output: "Active=yes\nLockedHint=no\nType=wayland\n", unlocked: true, graphical: true},
		{name: "locked X11", output: "Type=x11\nLockedHint=yes\nActive=yes\n", graphical: true},
		{name: "inactive graphical", output: "Active=no\nLockedHint=no\nType=wayland\n", unlocked: true, graphical: true},
		{name: "terminal", output: "Active=yes\nLockedHint=no\nType=tty\n", unlocked: true},
		{name: "missing property", output: "Active=yes\nType=wayland\n", wantErr: true},
		{name: "invalid boolean", output: "Active=maybe\nLockedHint=no\nType=wayland\n", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			active, unlocked, graphical, err := parseSessionState(test.output)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && (unlocked != test.unlocked || graphical != test.graphical || active != (test.name != "inactive graphical")) {
				t.Fatalf("state = active:%v unlocked:%v graphical:%v", active, unlocked, graphical)
			}
		})
	}
}
