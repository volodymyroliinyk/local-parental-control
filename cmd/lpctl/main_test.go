package main

import (
	"testing"

	"github.com/volodymyroliinyk/local-parental-control/internal/api"
)

func TestScheduleDescription(t *testing.T) {
	tests := []struct {
		name string
		user api.UserStatus
		want string
	}{
		{name: "all day", user: api.UserStatus{AllDay: true}, want: "all day"},
		{name: "clock window", user: api.UserStatus{AllowedFrom: "08:00", AllowedUntil: "20:00"}, want: "08:00-20:00"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := scheduleDescription(test.user); got != test.want {
				t.Fatalf("scheduleDescription() = %q, want %q", got, test.want)
			}
		})
	}
}
