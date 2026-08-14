package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	DatabaseURL               string `mapstructure:"DATABASE_URL"`
	DBHost                    string `mapstructure:"DB_HOST"`
	DBPort                    string `mapstructure:"DB_PORT"`
	DBSSLMode                 string `mapstructure:"DB_SSLMODE"`
	DBChannelBinding          string `mapstructure:"DB_CHANNEL_BINDING"`
	DBUser                    string `mapstructure:"DB_USER"`
	DBPassword                string `mapstructure:"DB_PASSWORD"`
	DBName                    string `mapstructure:"DB_NAME"`
	IdentityGRPCHost          string `mapstructure:"IDENTITY_GRPC_HOST"`
	BillingServiceURL         string `mapstructure:"BILLING_SERVICE_URL"`
	InternalServiceCredential string `mapstructure:"INTERNAL_SERVICE_CREDENTIAL"`
	Port                      string `mapstructure:"PORT"`
	JWTSecret                 string `mapstructure:"JWT_SECRET"`
}

func LoadConfig() (Config, error) {
	if err := godotenv.Load(); err != nil {
		slog.Warn("No .env file found by godotenv")
	}

	viper.SetConfigFile(".env")
	if err := viper.ReadInConfig(); err != nil {
		if !os.IsNotExist(err) {
			return Config{}, err
		}
		slog.Warn("No .env file found by Viper, using system environment variables")
	}

	viper.AutomaticEnv()
	for _, key := range []string{
		"DATABASE_URL", "DB_HOST", "DB_PORT", "DB_SSLMODE", "DB_CHANNEL_BINDING", "DB_USER", "DB_PASSWORD", "DB_NAME",
		"IDENTITY_GRPC_HOST", "BILLING_SERVICE_URL", "INTERNAL_SERVICE_CREDENTIAL", "PORT", "JWT_SECRET",
	} {
		if err := viper.BindEnv(key); err != nil {
			return Config{}, err
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return Config{}, err
	}
	if err := applyDatabaseURL(&config); err != nil {
		return Config{}, err
	}

	// Default fallback values
	if config.DBHost == "" {
		config.DBHost = "localhost"
	}
	if config.DBPort == "" {
		config.DBPort = "5432"
	}
	if config.DBSSLMode == "" {
		config.DBSSLMode = "disable"
	}
	if config.DBChannelBinding == "" {
		config.DBChannelBinding = "disable"
	}
	if err := validateChannelBinding(config.DBChannelBinding); err != nil {
		return Config{}, err
	}
	if config.DBUser == "" {
		config.DBUser = "postgres"
	}
	if config.DBPassword == "" {
		config.DBPassword = "postgres"
	}
	if config.DBName == "" {
		config.DBName = "kelolakelas_academic"
	}
	if config.IdentityGRPCHost == "" {
		config.IdentityGRPCHost = "localhost:50051"
	}
	if config.Port == "" {
		config.Port = "8081"
	}
	if config.JWTSecret == "" {
		config.JWTSecret = "supersecretjwtkey123!"
	}
	if config.BillingServiceURL == "" {
		config.BillingServiceURL = "http://localhost:8082"
	}
	if config.InternalServiceCredential == "" {
		return Config{}, fmt.Errorf("INTERNAL_SERVICE_CREDENTIAL is required")
	}

	return config, nil
}

func applyDatabaseURL(config *Config) error {
	if config.DatabaseURL == "" {
		return nil
	}
	databaseURL, err := url.Parse(config.DatabaseURL)
	if err != nil || (databaseURL.Scheme != "postgres" && databaseURL.Scheme != "postgresql") || databaseURL.Hostname() == "" || databaseURL.Path == "" {
		return fmt.Errorf("DATABASE_URL must be a valid PostgreSQL URL")
	}
	if config.DBHost == "" {
		config.DBHost = databaseURL.Hostname()
	}
	if config.DBPort == "" {
		config.DBPort = databaseURL.Port()
		if config.DBPort == "" {
			config.DBPort = "5432"
		}
	}
	if config.DBUser == "" && databaseURL.User != nil {
		config.DBUser = databaseURL.User.Username()
	}
	if config.DBPassword == "" && databaseURL.User != nil {
		config.DBPassword, _ = databaseURL.User.Password()
	}
	if config.DBName == "" {
		config.DBName = strings.TrimPrefix(databaseURL.Path, "/")
	}
	if config.DBSSLMode == "" {
		config.DBSSLMode = databaseURL.Query().Get("sslmode")
	}
	if config.DBChannelBinding == "" {
		config.DBChannelBinding = databaseURL.Query().Get("channel_binding")
	}
	return nil
}

func validateChannelBinding(value string) error {
	switch value {
	case "disable", "prefer", "require":
		return nil
	default:
		return fmt.Errorf("DB_CHANNEL_BINDING must be one of disable, prefer, or require")
	}
}
