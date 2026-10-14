package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, "[agent]\nmanifest_path = \"/etc/baseline/manifest.json\"\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HeartbeatIntervalSec != heartbeat_interval_sec_default ||
		cfg.AuditIntervalSec != audit_interval_sec_default ||
		cfg.AuditJitterMaxSec != audit_jitter_max_sec_default {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.NodeID != "" {
		t.Fatalf("node_id should stay empty so the hostname can win, got %q", cfg.NodeID)
	}
}

func TestLoadRequiresManifestPath(t *testing.T) {
	if _, err := Load(writeConfig(t, "[agent]\nnode_id = \"web-01\"\n")); err == nil {
		t.Fatal("expected an error when manifest_path is absent")
	}
}

// A key this build no longer honors (remediate was removed with daemon-mode
// enforcement) must not be fatal: a config that fails to load stops the
// heartbeat and turns a stale key into a dead-man alert.
func TestLoadToleratesRetiredKeys(t *testing.T) {
	cfg, err := Load(writeConfig(t, "[agent]\nmanifest_path = \"/etc/baseline/manifest.json\"\nremediate = true\n"))
	if err != nil {
		t.Fatalf("a retired key must not fail the load: %v", err)
	}
	if cfg.ManifestPath != "/etc/baseline/manifest.json" {
		t.Fatalf("known keys must still decode, got %+v", cfg)
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, `[agent]
manifest_path = "/etc/baseline/manifest.json"
heartbeat_interval_sec = 5
audit_interval_sec = 10
audit_jitter_max_sec = 2
check = ["sha256", "mode", "mtime"]
fast_scan = true
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HeartbeatIntervalSec != 5 || cfg.AuditIntervalSec != 10 || cfg.AuditJitterMaxSec != 2 {
		t.Fatalf("explicit intervals not honored: %+v", cfg)
	}
	if !cfg.FastScan || len(cfg.CheckSet) != 3 {
		t.Fatalf("check set / fast_scan not honored: %+v", cfg)
	}
}
