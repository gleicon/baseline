package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// querySettingValue reads a live runtime value for a settings key.
// Backends are hard-coded (no arbitrary commands):
//
//	sysctl:<name>  live kernel parameter via `sysctl -n <name>`
//	modules        sha256 of the sorted loaded kernel module names (/proc/modules)
//	bpf            sha256 of loaded eBPF program counts by type (needs bpftool)
func querySettingValue(key string) (string, error) {
	switch {
	case strings.HasPrefix(key, "sysctl:"):
		return querySysctl(strings.TrimPrefix(key, "sysctl:"))
	case key == "modules":
		return queryModules()
	case key == "bpf":
		return queryBPF()
	default:
		return "", fmt.Errorf("unsupported setting key %q", key)
	}
}

func querySysctl(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty sysctl name")
	}
	out, err := exec.Command("sysctl", "-n", name).Output()
	if err != nil {
		return "", err
	}
	val := strings.TrimSpace(string(out))
	if val == "" {
		return "", fmt.Errorf("empty value for sysctl %q", name)
	}
	return val, nil
}

func queryModules() (string, error) {
	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		return "", err
	}
	return digestStrings(moduleNames(string(data))), nil
}

func queryBPF() (string, error) {
	out, err := exec.Command("bpftool", "prog", "show").Output()
	if err != nil {
		return "", err
	}
	return digestStrings(progTypeCounts(string(out))), nil
}

// moduleNames extracts the module name (first field) from /proc/modules,
// sorted.
func moduleNames(procModules string) []string {
	var names []string
	for _, line := range strings.Split(procModules, "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && f[0] != "" {
			names = append(names, f[0])
		}
	}
	sort.Strings(names)
	return names
}

// progTypeCounts tallies loaded eBPF programs by type from `bpftool prog show`
// lines ("12: kprobe name ..."), returning sorted "type=count" entries.
func progTypeCounts(bpftoolOut string) []string {
	counts := map[string]int{}
	for _, line := range strings.Split(bpftoolOut, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[1] == "" {
			continue
		}
		counts[f[1]]++
	}
	out := make([]string, 0, len(counts))
	for typ, n := range counts {
		out = append(out, fmt.Sprintf("%s=%d", typ, n))
	}
	sort.Strings(out)
	return out
}

// digestStrings hashes a sorted set into a stable value.
func digestStrings(in []string) string {
	h := sha256.Sum256([]byte(strings.Join(in, "\n")))
	return hex.EncodeToString(h[:])
}

// isSysctlDeckKey reports whether key asks for the whole sysctl namespace
// ("sysctl:all") or a prefix of it ("sysctl:all:<prefix>"). Returns the base
// prefix to match against ("" for all).
func isSysctlDeckKey(key string) (prefix string, ok bool) {
	rest, found := strings.CutPrefix(key, "sysctl:all")
	if !found {
		return "", false
	}
	if rest == "" {
		return "", true // everything
	}
	prefix = strings.TrimPrefix(rest, ":")
	prefix = strings.TrimSuffix(prefix, ".*")
	return prefix, prefix != ""
}

// sysctlSnapshot runs `sysctl -a` and returns the name/value map, values
// trimmed.
func sysctlSnapshot() (map[string]string, error) {
	out, err := exec.Command("sysctl", "-a").Output()
	if err != nil {
		return nil, err
	}
	return parseSysctlAll(string(out)), nil
}

// parseSysctlAll parses `sysctl -a` output ("name = value" or "name: value").
func parseSysctlAll(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, " = ")
		if !ok {
			name, value, ok = strings.Cut(line, ": ")
			if !ok {
				continue
			}
		}
		m[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	return m
}

// sysctlNameInPrefix reports whether a sysctl name is under prefix "" (all),
// equals it, or is a dotted child of it.
func sysctlNameInPrefix(name, prefix string) bool {
	if prefix == "" {
		return true
	}
	return name == prefix || strings.HasPrefix(name, prefix+".")
}

// collectSysctlDeck records the sysctl namespace (or a prefix of it) into the
// manifest's sysctl_snapshot. Multiple keys merge. Returns warnings.
func collectSysctlDeck(key string, m *Manifest) []string {
	prefix, _ := isSysctlDeckKey(key)
	all, err := sysctlSnapshot()
	if err != nil || len(all) == 0 {
		return []string{fmt.Sprintf("sysctl namespace not queryable at provisioning time: %s", key)}
	}
	if m.SysctlSnapshot == nil {
		m.SysctlSnapshot = map[string]string{}
	}
	for name, value := range all {
		if sysctlNameInPrefix(name, prefix) {
			m.SysctlSnapshot[name] = value
		}
	}
	return nil
}

// checkSysctlSnapshot compares the recorded sysctl snapshot to the live
// namespace and reports one deviation per changed value. Unqueryable namespace
// reports setting:unavailable:sysctl:all.
func checkSysctlSnapshot(snapshot map[string]string) []string {
	return checkSysctlSnapshotLive(snapshot, sysctlSnapshot)
}

// checkSysctlSnapshotLive is checkSysctlSnapshot with an injectable live query
// (single subprocess per check), so it is deterministic to test.
func checkSysctlSnapshotLive(snapshot map[string]string, live func() (map[string]string, error)) []string {
	if len(snapshot) == 0 {
		return nil
	}
	current, err := live()
	if err != nil {
		return []string{"setting:unavailable:sysctl:all"}
	}
	names := make([]string, 0, len(snapshot))
	for name := range snapshot {
		names = append(names, name)
	}
	sort.Strings(names)
	var deviations []string
	for _, name := range names {
		have, ok := current[name]
		if !ok || have != strings.TrimSpace(snapshot[name]) {
			deviations = append(deviations, fmt.Sprintf("setting:mismatch:sysctl:%s:%s:%s", name, have, strings.TrimSpace(snapshot[name])))
		}
	}
	return deviations
}
