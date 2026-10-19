package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestParentNotificationConfig(t *testing.T) {
	for _, tc := range []struct {
		name, interval, from string
		disabled, wantError  bool
	}{
		{name: "defaults"}, {name: "disabled", disabled: true}, {name: "custom", interval: "30", from: "KelolaKelas <sender@example.test>"},
		{name: "zero interval", interval: "0", wantError: true}, {name: "unbounded interval", interval: "3601", wantError: true}, {name: "bad sender", from: "invalid", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			t.Chdir(t.TempDir())
			t.Setenv("JWT_SECRET", "test-jwt-secret")
			t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "test-credential")
			t.Setenv("PARENT_NOTIFICATION_WORKER_ENABLED", "true")
			t.Setenv("PARENT_NOTIFICATION_INTERVAL_SECONDS", "15")
			t.Setenv("RESEND_FROM_EMAIL", tc.from)
			t.Setenv("RESEND_API_KEY", "")
			if tc.disabled {
				t.Setenv("PARENT_NOTIFICATION_WORKER_ENABLED", "false")
			}
			if tc.interval != "" {
				t.Setenv("PARENT_NOTIFICATION_INTERVAL_SECONDS", tc.interval)
			}
			c, err := LoadConfig()
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			if err == nil && c.ParentNotificationWorkerEnabled == tc.disabled {
				t.Fatal("dispatch toggle ignored")
			}
		})
	}
}
