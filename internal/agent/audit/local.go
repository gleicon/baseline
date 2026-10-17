package audit

import (
	agentcfg "github.com/gleicon/baseline/internal/agent/config"
	"github.com/gleicon/baseline/internal/assert"
)

// RemediateLocal enforces cfg's manifest (packages and services only) and
// returns the verdict from re-checking afterwards, so the caller reports the
// post-enforcement state rather than the state that triggered it. The whole
// config is used for the re-check, so the check set and fast_scan apply
// exactly as they would in the daemon. Callers must have explicitly
// authorized remediation; there is no daemon equivalent.
func RemediateLocal(cfg *agentcfg.AgentConfig) (CheckResult, Remediation, error) {
	assert.True(cfg != nil, "cfg must not be nil")
	assert.True(cfg.ManifestPath != "", "manifest_path must not be empty")

	m, err := loadManifest(cfg.ManifestPath)
	if err != nil {
		return CheckResult{}, Remediation{}, err
	}
	rem := Remediate(m)
	return CheckManifest(m, cfg), rem, nil
}
