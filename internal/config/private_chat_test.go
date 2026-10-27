package config

import (
	"github.com/spf13/viper"
	"testing"
)

func TestPrivateRequestChatConfiguration(t *testing.T) {
	for _, tc := range []struct {
		interval, enabled string
		bad               bool
	}{{"5", "true", false}, {"1", "false", false}, {"0", "true", true}, {"3601", "true", true}, {"invalid", "true", true}, {"5", "invalid", true}} {
		t.Run(tc.interval+tc.enabled, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Chdir(t.TempDir())
			t.Setenv("JWT_SECRET", "fixture-secret")
			t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "fixture-internal")
			t.Setenv("PRIVATE_REQUEST_CHAT_INTERVAL_SECONDS", tc.interval)
			t.Setenv("PRIVATE_REQUEST_CHAT_WORKER_ENABLED", tc.enabled)
			cfg, err := LoadConfig()
			if (err != nil) != tc.bad {
				t.Fatalf("config %+v err%v", cfg, err)
			}
			if err == nil && tc.enabled == "false" && cfg.PrivateRequestChatWorkerEnabled {
				t.Fatal("dispatch could not be paused")
			}
		})
	}
}
