//go:build !linux && !darwin

package audit

import "errors"

// checkPackages and checkServices are no-ops on non-Linux platforms.
// The agent is designed for Linux fleet nodes; this stub allows the package
// to compile on macOS for development and testing.

func checkPackages(_ []ManifestPackage) []string { return nil }
func checkServices(_ []ManifestService) []string { return nil }

// The unauthorized checks are platform-independent (see manifest.go); only the
// live lookups they call are not. Reporting an error keeps them silent rather
// than claiming an empty system, which would flag every tracked item as drift.
var errNoLookup = errors.New("no package or service lookup on this platform")

func listInstalledPackages() ([]string, error) { return nil, errNoLookup }
func listActiveServices() ([]string, error)    { return nil, errNoLookup }

// Non-Linux stubs for manifest generation helpers (see manifest_linux.go).
// Generation is a Linux-target operation; on other platforms package/service
// lookups report absent so file-only manifests still work in dev.

func LookupPackageVersion(_ string) (string, bool) { return "", false }
func ListInstalledPackages() []string              { return nil }
func ListActiveServices() []string                 { return nil }

func QueryServiceStates(_ string) (running, enabled bool) { return false, false }
