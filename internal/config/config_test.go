package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGetEnv(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		t.Setenv("TEST_GET_ENV", "value")
		if got := getEnv("TEST_GET_ENV", "default"); got != "value" {
			t.Errorf("getEnv() = %q, want %q", got, "value")
		}
	})
	t.Run("unset", func(t *testing.T) {
		if got := getEnv("TEST_GET_ENV_UNSET", "default"); got != "default" {
			t.Errorf("getEnv() = %q, want %q", got, "default")
		}
	})
	t.Run("empty", func(t *testing.T) {
		t.Setenv("TEST_GET_ENV_EMPTY", "")
		if got := getEnv("TEST_GET_ENV_EMPTY", "default"); got != "default" {
			t.Errorf("getEnv() = %q, want %q", got, "default")
		}
	})
}

func TestGetBoolEnv(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		def   bool
		want  bool
	}{
		{name: "true", value: "true", set: true, def: false, want: true},
		{name: "false", value: "false", set: true, def: true, want: false},
		{name: "one", value: "1", set: true, def: false, want: true},
		{name: "zero", value: "0", set: true, def: true, want: false},
		{name: "unset", set: false, def: true, want: true},
		{name: "unparseable", value: "yes", set: true, def: false, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("TEST_GET_BOOL_ENV", tt.value)
			}
			if got := getBoolEnv("TEST_GET_BOOL_ENV", tt.def); got != tt.want {
				t.Errorf("getBoolEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetIntEnv(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		def   int
		want  int
	}{
		{name: "valid", value: "42", set: true, def: 0, want: 42},
		{name: "unset", set: false, def: 7, want: 7},
		{name: "unparseable", value: "abc", set: true, def: 7, want: 7},
		{name: "negative", value: "-3", set: true, def: 0, want: -3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("TEST_GET_INT_ENV", tt.value)
			}
			if got := getIntEnv("TEST_GET_INT_ENV", tt.def); got != tt.want {
				t.Errorf("getIntEnv() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetDurationEnv(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		def   time.Duration
		want  time.Duration
	}{
		{name: "seconds", value: "5s", set: true, def: 0, want: 5 * time.Second},
		{name: "minutes", value: "2m", set: true, def: 0, want: 2 * time.Minute},
		{name: "unset", set: false, def: 3 * time.Second, want: 3 * time.Second},
		{name: "unparseable", value: "abc", set: true, def: 3 * time.Second, want: 3 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("TEST_GET_DURATION_ENV", tt.value)
			}
			if got := getDurationEnv("TEST_GET_DURATION_ENV", tt.def); got != tt.want {
				t.Errorf("getDurationEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPositiveIntEnv(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		def   int
		want  int
	}{
		{name: "positive", value: "4", set: true, def: 1, want: 4},
		{name: "zero", value: "0", set: true, def: 1, want: 1},
		{name: "negative", value: "-1", set: true, def: 1, want: 1},
		{name: "unset", set: false, def: 1, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("TEST_GET_POSITIVE_INT_ENV", tt.value)
			}
			if got := getPositiveIntEnv("TEST_GET_POSITIVE_INT_ENV", tt.def); got != tt.want {
				t.Errorf("getPositiveIntEnv() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetPositiveDurationEnv(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		def   time.Duration
		want  time.Duration
	}{
		{name: "positive", value: "5s", set: true, def: time.Second, want: 5 * time.Second},
		{name: "zero", value: "0s", set: true, def: time.Second, want: time.Second},
		{name: "negative", value: "-5s", set: true, def: time.Second, want: time.Second},
		{name: "unset", set: false, def: time.Second, want: time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("TEST_GET_POSITIVE_DURATION_ENV", tt.value)
			}
			if got := getPositiveDurationEnv("TEST_GET_POSITIVE_DURATION_ENV", tt.def); got != tt.want {
				t.Errorf("getPositiveDurationEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetStringSliceEnv(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		t.Setenv("TEST_GET_STRING_SLICE_ENV", "a,b,c")
		got := getStringSliceEnv("TEST_GET_STRING_SLICE_ENV", nil)
		want := []string{"a", "b", "c"}
		if len(got) != len(want) {
			t.Fatalf("getStringSliceEnv() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("getStringSliceEnv()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})
	t.Run("trimmed", func(t *testing.T) {
		t.Setenv("TEST_GET_STRING_SLICE_ENV", " a , b ")
		got := getStringSliceEnv("TEST_GET_STRING_SLICE_ENV", nil)
		want := []string{"a", "b"}
		if len(got) != len(want) {
			t.Fatalf("getStringSliceEnv() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("getStringSliceEnv()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})
	t.Run("empties dropped", func(t *testing.T) {
		t.Setenv("TEST_GET_STRING_SLICE_ENV", "a,,b")
		got := getStringSliceEnv("TEST_GET_STRING_SLICE_ENV", nil)
		want := []string{"a", "b"}
		if len(got) != len(want) {
			t.Fatalf("getStringSliceEnv() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("getStringSliceEnv()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})
	t.Run("unset", func(t *testing.T) {
		def := []string{"x", "y"}
		got := getStringSliceEnv("TEST_GET_STRING_SLICE_ENV_UNSET", def)
		if len(got) != len(def) {
			t.Fatalf("getStringSliceEnv() = %v, want %v", got, def)
		}
		for i := range def {
			if got[i] != def[i] {
				t.Errorf("getStringSliceEnv()[%d] = %q, want %q", i, got[i], def[i])
			}
		}
	})
}

func TestReadConfigFile_Defaults(t *testing.T) {
	cf, err := readConfigFile("")
	if err != nil {
		t.Fatalf("readConfigFile() error = %v", err)
	}
	if cf.Workers.Concurrency != 4 {
		t.Errorf("Concurrency = %d, want 4", cf.Workers.Concurrency)
	}
	if cf.Workers.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want 5s", cf.Workers.PollInterval)
	}
	if cf.Workers.ExecutionTimeout != 2*time.Minute {
		t.Errorf("ExecutionTimeout = %v, want 2m", cf.Workers.ExecutionTimeout)
	}
}

func TestReadConfigFile_ValidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	content := "workers:\n  concurrency: 8\nactions:\n  allowed_namespaces: [openshift-monitoring]\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cf, err := readConfigFile(path)
	if err != nil {
		t.Fatalf("readConfigFile() error = %v", err)
	}
	if cf.Workers.Concurrency != 8 {
		t.Errorf("Concurrency = %d, want 8", cf.Workers.Concurrency)
	}
	want := []string{"openshift-monitoring"}
	if len(cf.Actions.AllowedNamespaces) != 1 || cf.Actions.AllowedNamespaces[0] != want[0] {
		t.Errorf("AllowedNamespaces = %v, want %v", cf.Actions.AllowedNamespaces, want)
	}
}

func TestReadConfigFile_ReadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	_, err := readConfigFile(path)
	if err == nil {
		t.Fatal("readConfigFile() error = nil, want non-nil")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("readConfigFile() error = %v, want wrapped fs.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), "failed to read") || !strings.Contains(err.Error(), path) {
		t.Errorf("readConfigFile() error = %q, want to contain %q and %q", err, "failed to read", path)
	}
}

func TestReadConfigFile_UnmarshalError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte(":\n  - [\":"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := readConfigFile(path)
	if err == nil {
		t.Fatal("readConfigFile() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "failed to unmarshal") || !strings.Contains(err.Error(), path) {
		t.Errorf("readConfigFile() error = %q, want to contain %q and %q", err, "failed to unmarshal", path)
	}
}

// clearLoadEnv unsets every environment variable Load reads, so tests can
// assert on defaults without interference from the host environment or
// other tests in this file.
func clearLoadEnv(t *testing.T) {
	t.Helper()
	vars := []string{
		"ROSA_TA_AUTH",
		"ROSA_TA_ENABLE_AUTH",
		"ROSA_TA_LISTEN_ADDR",
		"ROSA_TA_LOG_LEVEL",
		"ROSA_TA_LOG_JSON",
		"AWS_REGION",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"ROSA_TA_S3_BUCKET",
		"ROSA_TA_S3_KEY_PREFIX",
		"ROSA_TA_ALLOWED_ACCOUNTS",
		"ROSA_TA_ROLES_CONFIG",
		"ROSA_TA_JWK_CERT_FILE",
		"ROSA_TA_JWK_CERT_URL",
		"ROSA_TA_OCM_BASE_URL",
		"ROSA_TA_OCM_CLIENT_ID",
		"ROSA_TA_OCM_CLIENT_SECRET",
		"ROSA_TA_OCM_TOKEN",
		"DATABASE_URL",
		"ROSA_TA_WORKER_CONCURRENCY",
		"ROSA_TA_WORKER_POLL_INTERVAL",
		"ROSA_TA_WORKER_EXECUTION_TIMEOUT",
		"ROSA_TA_BACKPLANE_URL",
		"ROSA_TA_BACKPLANE_CLIENT_ID",
		"ROSA_TA_BACKPLANE_CLIENT_SECRET",
		"ROSA_TA_KUBECONFIG",
		"ROSA_TA_ALLOWED_NAMESPACES",
		"ROSA_TA_ALLOWED_SECRETS",
	}
	for _, v := range vars {
		t.Setenv(v, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearLoadEnv(t)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.LogJSON != false {
		t.Errorf("LogJSON = %v, want false", cfg.LogJSON)
	}
	if cfg.S3KeyPrefix != "trusted-actions" {
		t.Errorf("S3KeyPrefix = %q, want %q", cfg.S3KeyPrefix, "trusted-actions")
	}
	if cfg.RolesConfigPath != "configs/role_mapping.yaml" {
		t.Errorf("RolesConfigPath = %q, want %q", cfg.RolesConfigPath, "configs/role_mapping.yaml")
	}
	if cfg.OCMBaseURL != "https://api.openshift.com" {
		t.Errorf("OCMBaseURL = %q, want %q", cfg.OCMBaseURL, "https://api.openshift.com")
	}
	if cfg.WorkerConcurrency != 4 {
		t.Errorf("WorkerConcurrency = %d, want 4", cfg.WorkerConcurrency)
	}
	if cfg.WorkerPollInterval != 5*time.Second {
		t.Errorf("WorkerPollInterval = %v, want 5s", cfg.WorkerPollInterval)
	}
	if cfg.WorkerExecutionTimeout != 2*time.Minute {
		t.Errorf("WorkerExecutionTimeout = %v, want 2m", cfg.WorkerExecutionTimeout)
	}
	if cfg.AuthPolicy != EnabledAuthPolicy {
		t.Errorf("AuthPolicy = %v, want %v", cfg.AuthPolicy, EnabledAuthPolicy)
	}
}

func TestLoad_AuthPolicy(t *testing.T) {
	tests := []struct {
		name       string
		authValue  string
		setAuth    bool
		enableAuth string
		setEnable  bool
		want       AuthPolicy
		wantErr    bool
	}{
		{name: "enabled", authValue: "enabled", setAuth: true, want: EnabledAuthPolicy},
		{name: "disabled", authValue: "disabled", setAuth: true, want: DisabledAuthPolicy},
		{name: "ocmconfig", authValue: "ocmconfig", setAuth: true, want: OcmConfigAuthPolicy},
		{name: "enable auth false", setAuth: false, enableAuth: "false", setEnable: true, want: DisabledAuthPolicy},
		{name: "no flags", setAuth: false, setEnable: false, want: EnabledAuthPolicy},
		{name: "bogus", authValue: "bogus", setAuth: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearLoadEnv(t)
			if tt.setAuth {
				t.Setenv("ROSA_TA_AUTH", tt.authValue)
			}
			if tt.setEnable {
				t.Setenv("ROSA_TA_ENABLE_AUTH", tt.enableAuth)
			}

			cfg, err := Load("")
			if tt.wantErr {
				if err == nil {
					t.Fatal("Load() error = nil, want non-nil")
				}
				if !strings.Contains(err.Error(), "invalid ROSA_TA_AUTH value") {
					t.Errorf("Load() error = %q, want to contain %q", err, "invalid ROSA_TA_AUTH value")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.AuthPolicy != tt.want {
				t.Errorf("AuthPolicy = %v, want %v", cfg.AuthPolicy, tt.want)
			}
		})
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearLoadEnv(t)
	t.Setenv("ROSA_TA_LISTEN_ADDR", ":9090")
	t.Setenv("ROSA_TA_WORKER_CONCURRENCY", "7")
	t.Setenv("ROSA_TA_ALLOWED_NAMESPACES", "ns1,ns2")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.WorkerConcurrency != 7 {
		t.Errorf("WorkerConcurrency = %d, want 7", cfg.WorkerConcurrency)
	}
	want := []string{"ns1", "ns2"}
	if len(cfg.AllowedNamespaces) != len(want) {
		t.Fatalf("AllowedNamespaces = %v, want %v", cfg.AllowedNamespaces, want)
	}
	for i := range want {
		if cfg.AllowedNamespaces[i] != want[i] {
			t.Errorf("AllowedNamespaces[%d] = %q, want %q", i, cfg.AllowedNamespaces[i], want[i])
		}
	}
}
