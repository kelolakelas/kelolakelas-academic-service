package config

import (
	"log/slog"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	DBHost            string `mapstructure:"DB_HOST"`
	DBPort            string `mapstructure:"DB_PORT"`
	DBSSLMode         string `mapstructure:"DB_SSLMODE"`
	DBUser            string `mapstructure:"DB_USER"`
	DBPassword        string `mapstructure:"DB_PASSWORD"`
	DBName            string `mapstructure:"DB_NAME"`
	IdentityGRPCHost  string `mapstructure:"IDENTITY_GRPC_HOST"`
	BillingServiceURL string `mapstructure:"BILLING_SERVICE_URL"`
	Port              string `mapstructure:"PORT"`
	JWTSecret         string `mapstructure:"JWT_SECRET"`
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
	if err := viper.BindEnv("DB_SSLMODE"); err != nil {
		return Config{}, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
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

	return config, nil
}
