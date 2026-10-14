package audit

import (
	agentcfg "github.com/gleicon/baseline/internal/agent/config"
	"github.com/gleicon/baseline/internal/assert"
)

// CheckLocal runs a one-shot manifest drift check without any hub interaction.
// It builds a minimal agent config and reuses RunManifest, so local results
// match what the daemon would emit. Returns (result, false) only when
// manifestPath is empty.
func CheckLocal(manifestPath, nodeID string) (CheckResult, bool) {
	assert.True(manifestPath != "", "manifestPath must not be empty")
	if nodeID == "" {
		nodeID = "localhost"
	}
	cfg := &agentcfg.AgentConfig{
		NodeID:       nodeID,
		ManifestPath: manifestPath,
	}
	return RunManifest(cfg)
}

// RemediateLocal enforces the manifest at path (packages and services only)
// and returns the verdict from re-checking afterwards, so the caller reports
// the post-enforcement state rather than the state that triggered it. Callers
// must have explicitly authorized remediation; there is no daemon equivalent.
func RemediateLocal(path, nodeID string) (CheckResult, Remediation, error) {
	assert.True(path != "", "path must not be empty")

	m, err := loadManifest(path)
	if err != nil {
		return CheckResult{}, Remediation{}, err
	}
	if nodeID == "" {
		nodeID = "localhost"
	}
	rem := Remediate(m)
	cfg := &agentcfg.AgentConfig{NodeID: nodeID, ManifestPath: path}
	return CheckManifest(m, cfg), rem, nil
}
