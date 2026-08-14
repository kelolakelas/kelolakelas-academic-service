package config

import (
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
	t.Setenv("INTERNAL_SERVICE_CREDENTIAL", "test-internal-credential")
	t.Setenv("DB_CHANNEL_BINDING", "invalid")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected invalid channel binding configuration error")
	}
}

func TestLoadConfigReadsDisabledChannelBindingFromDatabaseURL(t *testing.T) {
	viper.Reset()
	t.Chdir(t.TempDir())
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
