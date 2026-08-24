package main

import (
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/volodymyroliinyk/local-parental-control/internal/api"
)

func TestStatusNotifierPropertiesHaveDBusSignatures(t *testing.T) {
	status := &api.UserStatus{DeviceLimitSeconds: 3600, ContinuousLimitSeconds: 600}
	properties := propertiesFor(status, "2026-08-24")[itemInterface]
	for name, property := range properties {
		if signature := dbus.SignatureOf(property.Value); signature.Empty() {
			t.Fatalf("property %s has an empty D-Bus signature", name)
		}
	}
	if got := dbus.SignatureOf(properties["ToolTip"].Value).String(); got != "(sa(iiay)ss)" {
		t.Fatalf("tooltip signature = %q", got)
	}
}

func TestPausedStatusDoesNotRequestAttention(t *testing.T) {
	if got := statusState(&api.UserStatus{DeviceBlocked: true}); got != "Active" {
		t.Fatalf("status = %q", got)
	}
}
