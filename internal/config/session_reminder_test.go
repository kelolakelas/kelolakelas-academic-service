package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestSessionReminderConfig(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
		wantError        bool
	}{
		{name: "defaults"}, {"disabled", "SESSION_REMINDER_WORKER_ENABLED", "false", false},
		{"custom ahead", "SESSION_REMINDER_DAYS_AHEAD", "3", false},
		{"custom width", "SESSION_REMINDER_WINDOW_DAYS", "2", false},
		{"custom interval", "SESSION_REMINDER_INTERVAL_SECONDS", "60", false},
		{"zero ahead", "SESSION_REMINDER_DAYS_AHEAD", "0", true}, {"large ahead", "SESSION_REMINDER_DAYS_AHEAD", "31", true},
		{"zero width", "SESSION_REMINDER_WINDOW_DAYS", "0", true}, {"large width", "SESSION_REMINDER_WINDOW_DAYS", "8", true},
		{"zero interval", "SESSION_REMINDER_INTERVAL_SECONDS", "0", true}, {"large interval", "SESSION_REMINDER_INTERVAL_SECONDS", "3601", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			t.Chdir(t.TempDir())
			t.Setenv("JWT_SECRET", "test-jwt-secret")
			t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "test-credential")
			t.Setenv("SESSION_REMINDER_WORKER_ENABLED", "true")
			t.Setenv("SESSION_REMINDER_DAYS_AHEAD", "1")
			t.Setenv("SESSION_REMINDER_WINDOW_DAYS", "1")
			t.Setenv("SESSION_REMINDER_INTERVAL_SECONDS", "900")
			if tc.key != "" {
				t.Setenv(tc.key, tc.value)
			}
			c, err := LoadConfig()
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			if tc.name == "defaults" && (c.SessionReminderDaysAhead != 1 || c.SessionReminderWindowDays != 1 || c.SessionReminderIntervalSeconds != 900 || !c.SessionReminderWorkerEnabled) {
				t.Fatal("wrong defaults")
			}
			if tc.name == "disabled" && c.SessionReminderWorkerEnabled {
				t.Fatal("disable ignored")
			}
		})
	}
}
