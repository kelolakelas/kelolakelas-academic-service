package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestServerTimeoutConfig(t *testing.T) {
	keys := []string{
		"SERVER_READ_HEADER_TIMEOUT_SECONDS", "SERVER_READ_TIMEOUT_SECONDS", "SERVER_WRITE_TIMEOUT_SECONDS",
		"SERVER_IDLE_TIMEOUT_SECONDS", "SERVER_SHUTDOWN_TIMEOUT_SECONDS",
	}
	defaults := [5]int{DefaultServerReadHeaderTimeout, DefaultServerReadTimeout, DefaultServerWriteTimeout, DefaultServerIdleTimeout, DefaultServerShutdownTimeout}
	for _, tc := range []struct {
		name    string
		values  [5]string
		want    [5]int
		wantErr string
	}{
		{name: "unset uses safe defaults", want: defaults},
		{name: "configured values are used", values: [5]string{"2", "20", "45", "90", "25"}, want: [5]int{2, 20, 45, 90, 25}},
		{name: "write timeout just above the billing client timeout is accepted", values: [5]string{"", "", "11"}, want: [5]int{5, 30, 11, 120, 15}},
		{name: "zero never disables a bound", values: [5]string{"0", "0", "0", "0", "0"}, want: defaults},
		{name: "negative falls back to default", values: [5]string{"-1", "-5", "-10", "-1", "-3"}, want: defaults},
		{name: "write timeout equal to the billing client timeout is rejected", values: [5]string{"", "", "10"}, wantErr: "must be greater than the billing client timeout"},
		{name: "write timeout below the billing client timeout is rejected", values: [5]string{"", "", "5"}, wantErr: "must be greater than the billing client timeout"},
		{name: "non-numeric header timeout is rejected", values: [5]string{"soon"}, wantErr: "SERVER_READ_HEADER_TIMEOUT_SECONDS"},
		{name: "duration overflow is rejected", values: [5]string{"", "", "", "", "9223372036854775807"}, wantErr: "too large for a duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			t.Chdir(t.TempDir())
			t.Setenv("DATABASE_URL", "")
			t.Setenv("JWT_SECRET", "test-jwt-secret")
			t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "test-internal-credential")
			for i, key := range keys {
				t.Setenv(key, tc.values[i])
			}
			cfg, err := LoadConfig()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := [5]int{cfg.ServerReadHeaderTimeout, cfg.ServerReadTimeout, cfg.ServerWriteTimeout, cfg.ServerIdleTimeout, cfg.ServerShutdownTimeout}
			if got != tc.want {
				t.Fatalf("timeouts=%v, want %v", got, tc.want)
			}
		})
	}
}
