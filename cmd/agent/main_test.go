package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gleicon/baseline/internal/agent/audit"
)

// makeManifest writes a baseline sentinel and records a manifest tracking it.
func makeManifest(t *testing.T) (manifest, sentinel string) {
	t.Helper()
	dir := t.TempDir()
	manifest = filepath.Join(dir, "manifest.json")
	sentinel = filepath.Join(dir, "sentinel")
	if err := os.WriteFile(sentinel, []byte("baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := audit.GenerateManifest(manifest, []string{sentinel}, nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	return manifest, sentinel
}

func TestRunCheckExitCodes(t *testing.T) {
	manifest, sentinel := makeManifest(t)

	if got := runCheck("", manifest, "text", "test-node", ""); got != 0 {
		t.Fatalf("clean manifest: expected exit 0, got %d", got)
	}

	if err := os.WriteFile(sentinel, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runCheck("", manifest, "text", "test-node", ""); got != 1 {
		t.Fatalf("drifted manifest: expected exit 1, got %d", got)
	}
}

func TestRunCheckUsageErrors(t *testing.T) {
	if got := runCheck("", "unused", "xml", "n", ""); got != 2 {
		t.Fatalf("invalid format: expected exit 2, got %d", got)
	}

	missing := filepath.Join(t.TempDir(), "nope.json")
	if got := runCheck("", missing, "text", "n", ""); got != 2 {
		t.Fatalf("missing manifest: expected exit 2, got %d", got)
	}

	// Config mode with no manifest in the config file.
	cfgMissing := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(cfgMissing, []byte("[agent]\nmanifest_path = \"/does/not/exist.json\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := runCheck(cfgMissing, "", "text", "n", ""); got != 2 {
		t.Fatalf("config-mode missing manifest: expected exit 2, got %d", got)
	}
}

func TestRunCheckConfigModeResolution(t *testing.T) {
	manifest, _ := makeManifest(t)
	cfg := filepath.Join(t.TempDir(), "agent.toml")
	body := "[agent]\nnode_id = \"cfg-node\"\nmanifest_path = " + quote(manifest) + "\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	// No -manifest: the config supplies manifest_path and node id.
	if got := runCheck(cfg, "", "text", "", ""); got != 0 {
		t.Fatalf("config-mode clean manifest: expected exit 0, got %d", got)
	}

	// -manifest wins over the config path.
	other := filepath.Join(filepath.Dir(manifest), "other.json")
	if _, _, err := audit.GenerateManifest(other, nil, nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if got := runCheck(cfg, other, "text", "", ""); got != 0 {
		t.Fatalf("flag-mode clean manifest: expected exit 0, got %d", got)
	}
}

func TestResolveCheckConfigRejectsMissingManifest(t *testing.T) {
	_, _, err := resolveCheckConfig("", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "no manifest to check") {
		t.Fatalf("expected missing-manifest error, got %v", err)
	}
}

// A one-shot check must apply the same comparison the daemon would. The check
// set and fast_scan live in agent.toml, so resolving a check must carry the
// whole config forward, not just the manifest path and node id. Dropping them
// made -check report OK on a node the daemon flagged as drifted.
func TestResolveCheckConfigCarriesCheckSettings(t *testing.T) {
	manifest, _ := makeManifest(t)
	path := filepath.Join(t.TempDir(), "agent.toml")
	body := "[agent]\nmanifest_path = " + quote(manifest) +
		"\ncheck = [\"sha256\", \"mode\", \"mtime\"]\nfast_scan = true\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := resolveCheckConfig(path, "", "", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(cfg.CheckSet) != 3 {
		t.Errorf("check set must survive resolution: got %v, want 3 attributes", cfg.CheckSet)
	}
	if !cfg.FastScan {
		t.Error("fast_scan must survive resolution, got false")
	}

	// The same must hold when -manifest is supplied alongside the config.
	cfg, _, err = resolveCheckConfig(path, manifest, "", "")
	if err != nil {
		t.Fatalf("resolve with -manifest: %v", err)
	}
	if len(cfg.CheckSet) != 3 || !cfg.FastScan {
		t.Errorf("-manifest must not discard check settings: checkset=%v fast_scan=%v", cfg.CheckSet, cfg.FastScan)
	}
}

// A config that will not parse must stay non-fatal when -manifest supplies
// the path itself: the check can run on flags alone. Only when the config is
// the sole source of a manifest path does a broken one abort.
func TestResolveCheckConfigBrokenConfigTolerated(t *testing.T) {
	manifest, _ := makeManifest(t)
	bad := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(bad, []byte("[agent]\nthis is not = = valid toml\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := resolveCheckConfig(bad, manifest, "", "")
	if err != nil {
		t.Fatalf("-manifest must survive an unparseable config: %v", err)
	}
	if cfg.ManifestPath != manifest {
		t.Errorf("manifest path: got %q, want %q", cfg.ManifestPath, manifest)
	}

	if _, _, err := resolveCheckConfig(bad, "", "", ""); err == nil {
		t.Error("an unparseable config with no -manifest must be fatal, got nil")
	}
}

func quote(s string) string {
	return "\"" + strings.ReplaceAll(s, "\\", "\\\\") + "\""
}
