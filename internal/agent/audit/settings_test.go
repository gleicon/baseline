package audit

import (
	"errors"
	"strings"
	"testing"
)

func fakeSettingQuery() func(name string) (string, error) {
	return func(name string) (string, error) {
		switch name {
		case "sysctl:net.ipv4.tcp_syncookies":
			return "1", nil
		case "sysctl:net.ipv4.ip_forward":
			return "0", nil
		}
		if strings.HasPrefix(name, "missing:") {
			return "", errors.New("query failed")
		}
		return "", errors.New("unknown key")
	}
}

func TestCheckSettings(t *testing.T) {
	query := fakeSettingQuery()

	if devs := checkSettings([]ManifestSetting{
		{Key: "sysctl:net.ipv4.tcp_syncookies", Value: "1"},
	}, query); len(devs) != 0 {
		t.Fatalf("matching value should pass, got %v", devs)
	}

	// Recorded value with padding is trimmed before comparison.
	if devs := checkSettings([]ManifestSetting{
		{Key: "sysctl:net.ipv4.ip_forward", Value: " 0 \n"},
	}, query); len(devs) != 0 {
		t.Fatalf("padding in recorded value should be trimmed, got %v", devs)
	}

	devs := checkSettings([]ManifestSetting{
		{Key: "sysctl:net.ipv4.ip_forward", Value: "1"},
	}, query)
	if len(devs) != 1 || !strings.HasPrefix(devs[0], "setting:mismatch:sysctl:net.ipv4.ip_forward:0:1") {
		t.Fatalf("expected mismatch deviation, got %v", devs)
	}

	devs = checkSettings([]ManifestSetting{
		{Key: "missing:kern.whatever", Value: "1"},
	}, query)
	if len(devs) != 1 || devs[0] != "setting:unavailable:missing:kern.whatever" {
		t.Fatalf("expected unavailable deviation, got %v", devs)
	}
}

func TestCollectSettings(t *testing.T) {
	query := fakeSettingQuery()

	m := Manifest{}
	warnings := collectSettings([]string{
		"sysctl:net.ipv4.tcp_syncookies",
		"missing:kern.whatever",
	}, &m, query)

	if len(m.Settings) != 1 || m.Settings[0].Key != "sysctl:net.ipv4.tcp_syncookies" || m.Settings[0].Value != "1" {
		t.Fatalf("collected settings wrong: %+v", m.Settings)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "missing:kern.whatever") {
		t.Fatalf("expected one warning for the unqueryable key, got %v", warnings)
	}
}

func TestQuerySettingValueRejectsUnknownBackend(t *testing.T) {
	if _, err := querySettingValue("foo:bar"); err == nil {
		t.Fatal("keys without a known backend must be rejected")
	}
}

func TestModuleNames(t *testing.T) {
	got := moduleNames("ip_tables 23040 0 - Live 0x0000000000000000\noverlay 139264 6 - Live 0x000\n")
	want := []string{"ip_tables", "overlay"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("moduleNames: got %v want %v", got, want)
	}
	// Empty /proc/modules yields an empty, stable set.
	if len(moduleNames("")) != 0 {
		t.Fatal("empty /proc/modules should yield no modules")
	}
}

func TestProgTypeCounts(t *testing.T) {
	out := "1: kprobe name a tag x gpl\n2: tracepoint name b tag y gpl\n3: kprobe name c tag z gpl\n"
	got := progTypeCounts(out)
	want := []string{"kprobe=2", "tracepoint=1"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("progTypeCounts: got %v want %v", got, want)
	}
}

func TestDigestStringsStableAndSensitive(t *testing.T) {
	a := digestStrings([]string{"x", "y"})
	if a == "" || len(a) != 64 {
		t.Fatalf("expected a 64-char hex digest, got %q", a)
	}
	if digestStrings([]string{"x", "y"}) != a {
		t.Fatal("digest must be deterministic")
	}
	if digestStrings([]string{"x", "z"}) == a {
		t.Fatal("a different set must produce a different digest")
	}
}

func TestQueryModulesOrBPFIsStableOrUnavailable(t *testing.T) {
	// Portable: on a Linux node the backends return a 64-char digest; elsewhere
	// (or without bpftool) they report unavailable. Either outcome is valid;
	// a panic or wrong length is not.
	for _, key := range []string{"modules", "bpf"} {
		val, err := querySettingValue(key)
		if err == nil {
			if len(val) != 64 {
				t.Fatalf("%s: expected a 64-char digest, got %q", key, val)
			}
		}
	}
}

func TestParseSysctlAll(t *testing.T) {
	got := parseSysctlAll("net.ipv4.ip_forward = 1\nnet.ipv4.tcp_syncookies = 1\nkernel.ostype: Linux\n")
	want := map[string]string{
		"net.ipv4.ip_forward":     "1",
		"net.ipv4.tcp_syncookies": "1",
		"kernel.ostype":           "Linux",
	}
	if len(got) != len(want) {
		t.Fatalf("parseSysctlAll: got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("parseSysctlAll: %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestIsSysctlDeckKey(t *testing.T) {
	for _, tc := range []struct {
		key    string
		prefix string
		deck   bool
	}{
		{"sysctl:all", "", true},
		{"sysctl:all:net.ipv4.*", "net.ipv4", true},
		{"sysctl:all:net.ipv4", "net.ipv4", true},
		{"sysctl:net.ipv4.ip_forward", "", false},
		{"modules", "", false},
	} {
		prefix, deck := isSysctlDeckKey(tc.key)
		if deck != tc.deck || (!tc.deck && tc.deck) || (tc.deck && prefix != tc.prefix) {
			t.Fatalf("isSysctlDeckKey(%q) = prefix %q deck %v, want prefix %q deck %v", tc.key, prefix, deck, tc.prefix, tc.deck)
		}
	}
}

func TestSysctlNameInPrefix(t *testing.T) {
	if !sysctlNameInPrefix("net.ipv4.ip_forward", "") {
		t.Fatal("empty prefix matches everything")
	}
	if !sysctlNameInPrefix("net.ipv4.ip_forward", "net.ipv4") {
		t.Fatal("dotted child must match base prefix")
	}
	if sysctlNameInPrefix("net.ipv6.conf.all.disable_ipv6", "net.ipv4") {
		t.Fatal("unrelated name must not match prefix")
	}
}

func TestCheckSysctlSnapshotLive(t *testing.T) {
	live := map[string]string{
		"net.ipv4.ip_forward":     "0",
		"net.ipv4.tcp_syncookies": "1",
	}
	inject := func() (map[string]string, error) { return live, nil }

	snap := map[string]string{
		"net.ipv4.ip_forward":     "0",
		"net.ipv4.tcp_syncookies": "1",
	}
	if devs := checkSysctlSnapshotLive(snap, inject); len(devs) != 0 {
		t.Fatalf("matching snapshot should pass, got %v", devs)
	}

	live["net.ipv4.ip_forward"] = "1" // admin flipped it
	devs := checkSysctlSnapshotLive(snap, inject)
	if len(devs) != 1 || devs[0] != "setting:mismatch:sysctl:net.ipv4.ip_forward:1:0" {
		t.Fatalf("expected per-key mismatch, got %v", devs)
	}

	if devs := checkSysctlSnapshotLive(snap, func() (map[string]string, error) {
		return nil, errors.New("no sysctl")
	}); len(devs) != 1 || devs[0] != "setting:unavailable:sysctl:all" {
		t.Fatalf("expected unavailable deviation, got %v", devs)
	}
}

func TestCollectSettingsRoutesDeck(t *testing.T) {
	m := Manifest{}
	warnings := collectSettings([]string{"sysctl:all:net.ipv4.*"}, &m, fakeSettingQuery())
	// The fake query cannot satisfy the deck (it shells sysctl -a), so a
	// warning is expected on hosts without sysctl -a; the key must not land in
	// the scalar settings list.
	if len(m.Settings) != 0 {
		t.Fatalf("deck keys must not become scalar settings: %v", m.Settings)
	}
	_ = warnings
}
