package sshdriver

import (
	"strings"
	"testing"

	"github.com/dbarena/benchctl/internal/config"
)

func remoteCfg() *config.Config {
	return &config.Config{
		Store: config.StoreConfig{
			Mode:      config.ModeRemote,
			URL:       "https://example.supabase.co",
			AnonKey:   "anon-key-value",
			AuthToken: "local-token-must-not-leak",
		},
		Metrics: config.MetricsConfig{
			Endpoint: "https://metrics.example.com",
			Token:    "metrics-token",
		},
		Username: "ci/nightly",
	}
}

// The driver instance has no config file, so anything missing from this command is
// simply unavailable to the remote process. Dropping the store mode or the anon
// key leaves it unable to reach the shared store, which silently breaks
// --async: the run writes state to the driver's own filesystem and local
// `benchctl status` never sees progress.
func TestBuildResumeCmdCarriesStoreSettings(t *testing.T) {
	got := buildResumeCmd(remoteCfg(), "driver-token", "run-123")

	for _, want := range []string{
		"BENCHCTL_STORE_MODE='remote'",
		"BENCHCTL_STORE_ANON_KEY='anon-key-value'",
		"BENCHCTL_STORE_URL='https://example.supabase.co'",
		"BENCHCTL_METRICS_ENDPOINT='https://metrics.example.com'",
		"BENCHCTL_METRICS_TOKEN='metrics-token'",
		"BENCHCTL_USERNAME='ci/nightly'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("resume command missing %s\ngot: %s", want, got)
		}
	}
}

// The remote process must use the driver-specific credential, not whatever auth
// token the local config happened to carry.
func TestBuildResumeCmdOverridesAuthToken(t *testing.T) {
	got := buildResumeCmd(remoteCfg(), "driver-token", "run-123")

	if !strings.Contains(got, "BENCHCTL_STORE_AUTH_TOKEN='driver-token'") {
		t.Errorf("driver token not used\ngot: %s", got)
	}
	if strings.Contains(got, "local-token-must-not-leak") {
		t.Errorf("local auth token leaked to the driver instance\ngot: %s", got)
	}
}

// In local store mode there is no remote store for the driver to write to, so
// no credential is needed.
func TestResolveDriverTokenLocalModeNeedsNoCredential(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ~/.benchctl/credentials

	cfg := &config.Config{Store: config.StoreConfig{Mode: config.ModeLocal}}
	got, err := resolveDriverToken(cfg)
	if err != nil {
		t.Fatalf("local mode must not require a driver token, got error: %v", err)
	}
	if got != "" {
		t.Errorf("local mode token = %q, want empty", got)
	}
}

// Remote mode keeps requiring a credential
func TestResolveDriverTokenRemoteModeRequiresCredential(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ~/.benchctl/credentials

	cfg := remoteCfg()
	cfg.Store.ServiceRoleKey = ""
	if _, err := resolveDriverToken(cfg); err == nil {
		t.Fatal("remote mode without a credential must fail")
	}

	cfg.Store.ServiceRoleKey = "service-key"
	got, err := resolveDriverToken(cfg)
	if err != nil || got != "service-key" {
		t.Errorf("resolveDriverToken = %q, %v; want service-key, nil", got, err)
	}
}

// In local mode there is no driver token, so the command must not carry any
// auth token
func TestBuildResumeCmdWithoutDriverTokenOmitsAuthToken(t *testing.T) {
	cfg := &config.Config{Store: config.StoreConfig{
		Mode:      config.ModeLocal,
		AuthToken: "local-token-must-not-leak",
	}}
	got := buildResumeCmd(cfg, "", "run-123")

	if strings.Contains(got, "BENCHCTL_STORE_AUTH_TOKEN") {
		t.Errorf("auth token variable must be omitted\ngot: %s", got)
	}
	if strings.Contains(got, "local-token-must-not-leak") {
		t.Errorf("local auth token leaked to the driver instance\ngot: %s", got)
	}
}

func TestBuildResumeCmdOmitsUnsetValues(t *testing.T) {
	cfg := &config.Config{Store: config.StoreConfig{Mode: config.ModeLocal}}
	got := buildResumeCmd(cfg, "driver-token", "run-123")

	for _, absent := range []string{"BENCHCTL_METRICS_ENDPOINT", "BENCHCTL_STORE_URL", "BENCHCTL_CONFIG_FILE"} {
		if strings.Contains(got, absent) {
			t.Errorf("expected %s to be omitted when empty\ngot: %s", absent, got)
		}
	}
}

// Sorted assignments keep the command stable, which keeps it diffable in logs
// and comparable in tests.
func TestBuildResumeCmdIsStable(t *testing.T) {
	first := buildResumeCmd(remoteCfg(), "driver-token", "run-123")
	for i := 0; i < 10; i++ {
		if got := buildResumeCmd(remoteCfg(), "driver-token", "run-123"); got != first {
			t.Fatalf("command not stable across calls\nfirst: %s\ngot:   %s", first, got)
		}
	}
}

// Values reach the driver instance inside a shell command, so an embedded quote or
// semicolon must not be able to end the assignment and start a new command.
func TestBuildResumeCmdQuotesValues(t *testing.T) {
	cfg := remoteCfg()
	cfg.Username = "it's a name; rm -rf /"
	got := buildResumeCmd(cfg, "driver-token", "run-123")

	want := `BENCHCTL_USERNAME='it'\''s a name; rm -rf /'`
	if !strings.Contains(got, want) {
		t.Errorf("value not shell-quoted safely\nwant substring: %s\ngot:            %s", want, got)
	}
	if !strings.Contains(got, "resume 'run-123'") {
		t.Errorf("run ID not quoted\ngot: %s", got)
	}
}

// TestBuildStateImportCmd pins the two parts that make seeding work: the
// record goes into the driver's own local store, not whatever store the
// orchestrator uses, and it is read from the file Bootstrap scp'd over.
func TestBuildStateImportCmd(t *testing.T) {
	got := buildStateImportCmd()
	for _, want := range []string{"BENCHCTL_STORE_MODE=local", "~/benchctl/benchctl", "state import", "~/benchctl/state.json"} {
		if !strings.Contains(got, want) {
			t.Errorf("buildStateImportCmd() = %q, should contain %q", got, want)
		}
	}
}
