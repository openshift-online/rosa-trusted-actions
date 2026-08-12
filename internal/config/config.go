package config

import (
	"os"
	"strconv"
	"strings"
	"time"
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
	AllowedAccounts []string
	RolesConfigPath string
	JWKCertFile     string
	JWKCertURL      string

	// OCM Client Configuration (environment variables)
	OCMBaseURL      string
	OCMClientID     string
	OCMClientSecret string
	OCMToken        string

	// Database Configuration (environment variables, for future use)
	DatabaseURL string

	// Worker Configuration (environment variables)
	// WorkerConcurrency is the number of goroutines dequeuing and running
	// executions concurrently.
	WorkerConcurrency int
	// WorkerPollInterval is the fallback poll interval a worker uses to check
	// for pending executions when it hasn't been notified of new work.
	WorkerPollInterval time.Duration

	// Backplane Configuration
	BackplaneURL          string
	BackplaneClientID     string
	BackplaneClientSecret string

	// Kubernetes Configuration (local testing only)
	Kubeconfig string

	// Authorization Configuration
	AllowedNamespaces []string
	AllowedSecrets    []string

	// Development / local-testing flags
	// EnableAuth controls whether OCM JWT validation and AMS role checks are
	// enforced. Defaults to true. Set ROSA_TA_ENABLE_AUTH=false to use the
	// hardcoded mock identity ("dev-user" / SREP role) — never use in production.
	EnableAuth bool
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
		AllowedAccounts: getStringSliceEnv("ROSA_TA_ALLOWED_ACCOUNTS", nil),
		RolesConfigPath: getEnv("ROSA_TA_ROLES_CONFIG", "configs/role_mapping.yaml"),
		JWKCertFile:     getEnv("ROSA_TA_JWK_CERT_FILE", ""),
		JWKCertURL:      getEnv("ROSA_TA_JWK_CERT_URL", "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/certs"),

		// OCM Client Configuration
		OCMBaseURL:      getEnv("ROSA_TA_OCM_BASE_URL", "https://api.openshift.com"),
		OCMClientID:     getEnv("ROSA_TA_OCM_CLIENT_ID", ""),
		OCMClientSecret: getEnv("ROSA_TA_OCM_CLIENT_SECRET", ""),
		OCMToken:        getEnv("ROSA_TA_OCM_TOKEN", ""),

		// Database Configuration
		DatabaseURL: getEnv("DATABASE_URL", ""),

		// Worker Configuration
		WorkerConcurrency:  getIntEnv("ROSA_TA_WORKER_CONCURRENCY", 4),
		WorkerPollInterval: getDurationEnv("ROSA_TA_WORKER_POLL_INTERVAL", 5*time.Second),

		// Backplane Configuration
		BackplaneURL:          getEnv("ROSA_TA_BACKPLANE_URL", ""),
		BackplaneClientID:     getEnv("ROSA_TA_BACKPLANE_CLIENT_ID", ""),
		BackplaneClientSecret: getEnv("ROSA_TA_BACKPLANE_CLIENT_SECRET", ""),

		// Kubernetes Configuration (local testing only)
		Kubeconfig: getEnv("ROSA_TA_KUBECONFIG", ""),

		// Authorization Configuration
		AllowedNamespaces: getStringSliceEnv("ROSA_TA_ALLOWED_NAMESPACES", nil),
		AllowedSecrets:    getStringSliceEnv("ROSA_TA_ALLOWED_SECRETS", nil),

		// Development flags
		EnableAuth: getBoolEnv("ROSA_TA_ENABLE_AUTH", true),
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

// getIntEnv gets an integer environment variable with a default value
func getIntEnv(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// getDurationEnv gets a duration environment variable (e.g. "5s") with a default value
func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// getStringSliceEnv gets a comma-separated string as a slice with a default value
func getStringSliceEnv(key string, defaultValue []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
