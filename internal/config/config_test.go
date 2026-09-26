package config

import (
	"os"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*testing.T)
		wantHost    string
		wantPort    string
		wantDBName  string
		wantGRPC    string
		wantBinding string
	}{
		{
			name: "environment variables are loaded",
			setup: func(t *testing.T) {
				t.Setenv("DB_HOST", "railway-db.internal")
				t.Setenv("PORT", "19081")
				t.Setenv("IDENTITY_GRPC_HOST", "identity.railway.internal:50051")
			},
			wantHost: "railway-db.internal", wantPort: "19081", wantDBName: "kelolakelas_academic", wantGRPC: "identity.railway.internal:50051", wantBinding: "disable",
		},
		{
			name:     "environment overrides defaults",
			setup:    func(t *testing.T) { t.Setenv("PORT", "49153") },
			wantHost: "localhost", wantPort: "49153", wantDBName: "kelolakelas_academic", wantGRPC: "localhost:50051", wantBinding: "disable",
		},
		{
			name:        "missing optional variables use defaults",
			wantHost:    "localhost",
			wantPort:    "8081",
			wantDBName:  "kelolakelas_academic",
			wantGRPC:    "localhost:50051",
			wantBinding: "disable",
		},
		{
			name: "DATABASE_URL supplies database settings",
			setup: func(t *testing.T) {
				t.Setenv("DATABASE_URL", "postgres://academic_user:academic_password@postgres.internal:6543/academic_db?sslmode=require&channel_binding=require")
			},
			wantHost: "postgres.internal", wantPort: "8081", wantDBName: "academic_db", wantGRPC: "localhost:50051", wantBinding: "require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Chdir(t.TempDir())
			for _, key := range []string{"DATABASE_URL", "DB_HOST", "DB_PORT", "DB_SSLMODE", "DB_CHANNEL_BINDING", "DB_USER", "DB_PASSWORD", "DB_NAME", "IDENTITY_GRPC_HOST", "BILLING_SERVICE_URL", "INTERNAL_SERVICE_CREDENTIAL", "PORT", "JWT_SECRET"} {
				t.Setenv(key, "")
			}
			t.Setenv("JWT_SECRET", "test-jwt-secret")
			t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "test-internal-credential")
			if tt.setup != nil {
				tt.setup(t)
			}

			config, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if config.DBHost != tt.wantHost || config.Port != tt.wantPort || config.DBName != tt.wantDBName || config.IdentityGRPCHost != tt.wantGRPC || config.DBChannelBinding != tt.wantBinding {
				t.Fatalf("config database=%s port=%s name=%s grpc=%s, want database=%s port=%s name=%s grpc=%s", config.DBHost, config.Port, config.DBName, config.IdentityGRPCHost, tt.wantHost, tt.wantPort, tt.wantDBName, tt.wantGRPC)
			}
		})
	}
}

func TestChannelBindingEnvironmentOverridesDatabaseURL(t *testing.T) {
	viper.Reset()
	t.Chdir(t.TempDir())
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "test-internal-credential")
	t.Setenv("DATABASE_URL", "postgres://user:password@localhost/db?channel_binding=require")
	t.Setenv("DB_CHANNEL_BINDING", "disable")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.DBChannelBinding != "disable" {
		t.Fatalf("channel binding=%q, want disable", config.DBChannelBinding)
	}
}

func TestLoadConfigRejectsInvalidChannelBinding(t *testing.T) {
	viper.Reset()
	t.Chdir(t.TempDir())
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "test-internal-credential")
	t.Setenv("DB_CHANNEL_BINDING", "invalid")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected invalid channel binding configuration error")
	}
}

func TestLoadConfigReadsDisabledChannelBindingFromDatabaseURL(t *testing.T) {
	viper.Reset()
	t.Chdir(t.TempDir())
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "test-internal-credential")
	t.Setenv("DATABASE_URL", "postgres://user:password@localhost/db?sslmode=disable&channel_binding=disable")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.DBChannelBinding != "disable" {
		t.Fatalf("channel binding=%q, want disable", config.DBChannelBinding)
	}
}

func TestLoadConfigIdentityPermissionTimeout(t *testing.T) {
	tests := []struct {
		name    string
		value   *string
		want    int
		wantErr bool
	}{
		{name: "unset uses default", want: DefaultIdentityPermissionTimeoutMs},
		{name: "valid value is used", value: strPtr("1500"), want: 1500},
		{name: "zero falls back to default", value: strPtr("0"), want: DefaultIdentityPermissionTimeoutMs},
		{name: "negative falls back to default", value: strPtr("-250"), want: DefaultIdentityPermissionTimeoutMs},
		{name: "non-numeric value is rejected", value: strPtr("soon"), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			viper.Reset()
			t.Chdir(t.TempDir())
			t.Setenv("JWT_SECRET", "test-jwt-secret")
			t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "test-internal-credential")
			if test.value != nil {
				t.Setenv("IDENTITY_PERMISSION_TIMEOUT_MS", *test.value)
			} else {
				unsetEnv(t, "IDENTITY_PERMISSION_TIMEOUT_MS")
			}

			config, err := LoadConfig()
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected IDENTITY_PERMISSION_TIMEOUT_MS configuration error, got %d", config.IdentityPermissionTimeoutMs)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if config.IdentityPermissionTimeoutMs != test.want {
				t.Fatalf("IdentityPermissionTimeoutMs=%d, want %d", config.IdentityPermissionTimeoutMs, test.want)
			}
		})
	}
}

func strPtr(value string) *string { return &value }

// unsetEnv removes key for the duration of the test and restores its previous value.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigRequiresNonBlankJWTSecret(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		wantErr bool
	}{
		{name: "missing secret", wantErr: true},
		{name: "blank secret", secret: " \t ", wantErr: true},
		{name: "valid secret", secret: "test-jwt-secret"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			viper.Reset()
			t.Chdir(t.TempDir())
			t.Setenv("JWT_SECRET", test.secret)
			t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "test-internal-credential")

			config, err := LoadConfig()
			if test.wantErr {
				if err == nil {
					t.Fatal("expected JWT_SECRET configuration error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if config.JWTSecret != test.secret {
				t.Fatalf("JWTSecret=%q, want %q", config.JWTSecret, test.secret)
			}
		})
	}
}
