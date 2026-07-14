package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds the application configuration
type Config struct {
	// Server configuration (CLI flags)
	ListenAddr string
	LogLevel   string
	LogJSON    bool

	// AWS Configuration (environment variables)
	AWSRegion          string
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSSessionToken    string

	// Storage Configuration (environment variables)
	S3Bucket    string
	S3KeyPrefix string

	// Security Configuration (environment variables)
	EnableAuth      bool
	AllowedAccounts []string

	// Database Configuration (environment variables, for future use)
	DatabaseURL string
}

// Load loads configuration from environment variables with defaults
func Load() *Config {
	return &Config{
		// Server config (set via CLI flags, defaults here for reference)
		ListenAddr: getEnv("ROSA_TA_LISTEN_ADDR", ":8080"),
		LogLevel:   getEnv("ROSA_TA_LOG_LEVEL", "info"),
		LogJSON:    getBoolEnv("ROSA_TA_LOG_JSON", false),

		// AWS Configuration
		AWSRegion:          getEnv("AWS_REGION", ""),
		AWSAccessKeyID:     getEnv("AWS_ACCESS_KEY_ID", ""),
		AWSSecretAccessKey: getEnv("AWS_SECRET_ACCESS_KEY", ""),
		AWSSessionToken:    getEnv("AWS_SESSION_TOKEN", ""),

		// Storage Configuration
		S3Bucket:    getEnv("ROSA_TA_S3_BUCKET", ""),
		S3KeyPrefix: getEnv("ROSA_TA_S3_KEY_PREFIX", "trusted-actions"),

		// Security Configuration
		EnableAuth:      getBoolEnv("ROSA_TA_ENABLE_AUTH", true),
		AllowedAccounts: getStringSliceEnv("ROSA_TA_ALLOWED_ACCOUNTS", nil),

		// Database Configuration
		DatabaseURL: getEnv("DATABASE_URL", ""),
	}
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getBoolEnv gets a boolean environment variable with a default value
func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// getStringSliceEnv gets a comma-separated string as a slice with a default value
func getStringSliceEnv(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
