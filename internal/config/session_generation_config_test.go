package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func loadSessionGenerationConfig(t *testing.T, env map[string]string) (Config, error) {
	t.Helper()
	viper.Reset()
	t.Chdir(t.TempDir())
	for _, key := range []string{"SESSION_GENERATION_WORKER_ENABLED", "SESSION_GENERATION_INTERVAL_MINUTES", "SESSION_GENERATION_HORIZON_MONTHS", "DATABASE_URL"} {
		t.Setenv(key, "")
	}
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "test-internal-credential")
	for key, value := range env {
		t.Setenv(key, value)
	}
	return LoadConfig()
}

func TestSessionGenerationDefaults(t *testing.T) {
	cfg, err := loadSessionGenerationConfig(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SessionGenerationWorkerEnabled {
		t.Fatal("worker must be enabled by default")
	}
	if cfg.SessionGenerationIntervalMinutes != DefaultSessionGenerationIntervalMinutes || cfg.SessionGenerationHorizonMonths != DefaultSessionGenerationHorizonMonths {
		t.Fatalf("interval=%d horizon=%d", cfg.SessionGenerationIntervalMinutes, cfg.SessionGenerationHorizonMonths)
	}
}

func TestSessionGenerationOverrides(t *testing.T) {
	cfg, err := loadSessionGenerationConfig(t, map[string]string{
		"SESSION_GENERATION_WORKER_ENABLED":   "false",
		"SESSION_GENERATION_INTERVAL_MINUTES": "15",
		"SESSION_GENERATION_HORIZON_MONTHS":   "3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionGenerationWorkerEnabled || cfg.SessionGenerationIntervalMinutes != 15 || cfg.SessionGenerationHorizonMonths != 3 {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestSessionGenerationNonPositiveValuesUseDefaults(t *testing.T) {
	cfg, err := loadSessionGenerationConfig(t, map[string]string{
		"SESSION_GENERATION_INTERVAL_MINUTES": "-5",
		"SESSION_GENERATION_HORIZON_MONTHS":   "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionGenerationIntervalMinutes != DefaultSessionGenerationIntervalMinutes || cfg.SessionGenerationHorizonMonths != DefaultSessionGenerationHorizonMonths {
		t.Fatalf("interval=%d horizon=%d", cfg.SessionGenerationIntervalMinutes, cfg.SessionGenerationHorizonMonths)
	}
}

func TestSessionGenerationRejectsOutOfRangeValues(t *testing.T) {
	for _, tc := range []struct {
		key, value string
	}{
		{"SESSION_GENERATION_HORIZON_MONTHS", "13"},
		{"SESSION_GENERATION_INTERVAL_MINUTES", "9223372036854775807"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			_, err := loadSessionGenerationConfig(t, map[string]string{tc.key: tc.value})
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("err=%v, want an error naming %s", err, tc.key)
			}
		})
	}
}
