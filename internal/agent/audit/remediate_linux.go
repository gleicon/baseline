//go:build linux

package audit

import (
	"fmt"
	"os/exec"
)

var errNoPackageManager = fmt.Errorf("no package manager found")

// Remediate enforces the manifest as the source of truth for packages and
// services: it removes installed packages that are not in the package
// snapshot, stops/disables services not in the service snapshot, and brings
// declared services back to their recorded running/enabled state. File
// deviations are intentionally out of scope (file content is restored by the
// provisioning playbook — IaS is the source of truth for files).
//
// !!! SCOPE WARNING !!!
// This covers ONLY packages & services recorded in the manifest snapshot. It
// does NOT and cannot restore any other state a config-management system
// (Ansible, etc.) manages — users/groups, cron jobs, firewall rules, sysctl
// parameters, SELinux contexts, drop-ins, capabilities, custom modules. The
// manifest is not a full system-state model; re-running the provisioning
// playbook is the only complete remediation.
//
// Auto-remediation is NEVER on by default; callers must have explicitly
// authorized it (-remediate).
func Remediate(m Manifest) Remediation {
	var rem Remediation
	rem.PackagesRemoved = remediatePackages(m.PackageSnapshot)
	remediateServices(&rem, m.ServiceSnapshot, m.Services)
	return rem
}

// remediatePackages removes every installed package not in the snapshot. One
// apt-get invocation removes the whole set so apt resolves the dependency
// graph (curl's deep deps depend on each other and a per-package loop leaves
// orphans behind), then autoremove clears the leftovers. The report is
// reconciled against the post-run state so only packages actually removed are
// recorded (apt may keep protected/essential ones).
func remediatePackages(snapshot []string) []string {
	if len(snapshot) == 0 {
		return nil
	}
	current, err := listInstalledPackages()
	if err != nil {
		return nil
	}
	unauthorized := unauthorizedSet(snapshot, current)
	if len(unauthorized) == 0 {
		return nil
	}
	if hasBin("apt-get") {
		args := append([]string{"remove", "-y", "--purge"}, unauthorized...)
		exec.Command("apt-get", args...).Run()
		exec.Command("apt-get", "autoremove", "-y", "--purge").Run()
	} else {
		for _, p := range unauthorized {
			removePackage(p)
		}
	}
	after, err := listInstalledPackages()
	if err != nil {
		return nil
	}
	still := make(map[string]struct{}, len(after))
	for _, p := range after {
		still[p] = struct{}{}
	}
	var removed []string
	for _, p := range unauthorized {
		if _, ok := still[p]; !ok {
			removed = append(removed, p)
		}
	}
	return removed
}

// serviceIs reports whether systemd answers yes to a --quiet query
// ("is-active", "is-enabled"). A query that cannot run reads as "no".
func serviceIs(name, query string) bool {
	return exec.Command("systemctl", "--system", query, "--quiet", name).Run() == nil
}

// reconcile brings one systemd property of a service to want, and reports
// whether it actually changed. The error return of the fix-up command is the
// only evidence that the change took effect: under a restricted unit (no
// privileges) or on a static/masked unit these commands fail, and a failure
// must never be reported as a remediation. A property that resists the change
// keeps flagging as a deviation on the next check, so nothing is lost.
func reconcile(name, query string, want bool, on, off func(string) error) bool {
	switch is := serviceIs(name, query); {
	case want && !is:
		return on(name) == nil
	case !want && is:
		return off(name) == nil
	}
	return false
}

// remediateServices stops/disables services not in the service snapshot and
// brings declared services back to their recorded running/enabled state.
func remediateServices(rem *Remediation, serviceSnapshot []string, declared []ManifestService) {
	if len(serviceSnapshot) > 0 {
		current, err := listActiveServices()
		if err == nil {
			for _, s := range unauthorizedSet(serviceSnapshot, current) {
				if reconcile(s, "is-active", false, startService, stopService) {
					rem.ServicesStopped = append(rem.ServicesStopped, s)
				}
				if reconcile(s, "is-enabled", false, enableService, disableService) {
					rem.ServicesDisabled = append(rem.ServicesDisabled, s)
				}
			}
		}
	}

	for _, s := range declared {
		if reconcile(s.Name, "is-active", s.Running, startService, stopService) {
			if s.Running {
				rem.ServicesStarted = append(rem.ServicesStarted, s.Name)
			} else {
				rem.ServicesStopped = append(rem.ServicesStopped, s.Name)
			}
		}
		if reconcile(s.Name, "is-enabled", s.Enabled, enableService, disableService) {
			if s.Enabled {
				rem.ServicesEnabled = append(rem.ServicesEnabled, s.Name)
			} else {
				rem.ServicesDisabled = append(rem.ServicesDisabled, s.Name)
			}
		}
	}
}

func removePackage(name string) error {
	switch {
	case hasBin("apt-get"):
		return exec.Command("apt-get", "remove", "-y", "--allow-change-held-packages", name).Run()
	case hasBin("dnf"):
		return exec.Command("dnf", "remove", "-y", name).Run()
	case hasBin("yum"):
		return exec.Command("yum", "remove", "-y", name).Run()
	case hasBin("dpkg"):
		return exec.Command("dpkg", "-r", "--force-depends", name).Run()
	case hasBin("rpm"):
		return exec.Command("rpm", "-e", name).Run()
	}
	return errNoPackageManager
}

func hasBin(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func startService(name string) error {
	return exec.Command("systemctl", "--system", "start", name).Run()
}

func stopService(name string) error {
	return exec.Command("systemctl", "--system", "stop", name).Run()
}

func enableService(name string) error {
	return exec.Command("systemctl", "--system", "enable", name).Run()
}

func disableService(name string) error {
	return exec.Command("systemctl", "--system", "disable", name).Run()
}
