package rta

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetVersionInfo_Defaults(t *testing.T) {
	info := getVersionInfo()
	if info.Commit != "unknown" {
		t.Errorf("commit = %q, want unknown for unstamped build", info.Commit)
	}
	if info.BuildDate != "unknown" {
		t.Errorf("build_date = %q, want unknown", info.BuildDate)
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("go_version = %q, want %q", info.GoVersion, runtime.Version())
	}
	expected := runtime.GOOS + "/" + runtime.GOARCH
	if info.Platform != expected {
		t.Errorf("platform = %q, want %q", info.Platform, expected)
	}
}

func TestVersionInfo_String(t *testing.T) {
	info := versionInfo{
		Version: "v1.0.0", Commit: "abc123", BuildDate: "2026-01-01",
		GoVersion: "go1.26", Platform: "linux/amd64",
	}
	s := info.String()
	for _, want := range []string{"rta version: v1.0.0", "commit: abc123", "build date: 2026-01-01", "go: go1.26", "platform: linux/amd64"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() missing %q:\n%s", want, s)
		}
	}
}
