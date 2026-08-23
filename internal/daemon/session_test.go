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
