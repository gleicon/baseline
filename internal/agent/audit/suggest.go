package audit

import "strings"

// Baseline detects and reports; it never changes the node. Every deviation
// therefore ends in a human decision, and the agent's job is to make that
// decision cheap by naming the two options: restore the promised state, or
// decide the current state is correct and re-record the promise.
//
// Suggestions are advice, never commands run on the operator's behalf.

// suggestion is the advice attached to one class of deviation.
type suggestion struct {
	prefix string
	advice string
}

// suggestions are matched longest-prefix-first, so a specific deviation wins
// over the general one for its class.
var suggestions = []suggestion{
	{"file:hash_mismatch:", "file content changed — re-run the provisioning playbook to restore it, or re-record the manifest if the new content is correct"},
	{"file:missing:", "tracked file is gone — re-run the provisioning playbook to put it back, or drop it from the manifest if it is no longer managed"},
	{"file:mode_mismatch:", "permissions changed — re-apply them from the playbook (chmod alone will drift again at the next run)"},
	{"file:is_directory:", "a tracked file was replaced by a directory — investigate before re-recording; this is rarely accidental"},
	{"file:size_mismatch:", "file content changed — re-run the provisioning playbook to restore it"},
	{"file:mtime_mismatch:", "file was rewritten with identical content — usually a cron job, logrotate, or a tool rewriting in place"},
	{"pkg:missing:", "a promised package is not installed — re-run the provisioning playbook"},
	{"pkg:version_mismatch:", "package version differs from the promise — re-run the playbook, or re-record if the new version is intended"},
	{"pkg:unauthorized:", "package installed outside the baseline — remove it with your package manager, or re-run the playbook to adopt it as the new promise"},
	{"svc:unauthorized:", "service running outside the baseline — stop and disable it, or re-record if it belongs here"},
	{"svc:inactive:", "a promised service is not running — start it, or re-run the provisioning playbook"},
	{"svc:disabled:", "a promised service is not enabled — enable it, or re-run the provisioning playbook"},
	{"svc:unverifiable:", "systemd could not be queried — grant the service user access to the systemd manager, or run the check as root"},
	{"setting:mismatch:", "a tracked runtime value changed — re-apply it through your configuration management; a manual sysctl will not survive a reboot"},
	{"setting:unavailable:", "a tracked runtime value could not be read — the key may have been removed, or the agent lacks permission"},
	{"manifest:", "the manifest's own integrity is in question — see the signing section of the drift-operations guide before trusting any other verdict"},
}

// SuggestFor returns the advice for one deviation, or "" when none applies.
func SuggestFor(deviation string) string {
	best := ""
	bestLen := 0
	for _, s := range suggestions {
		if strings.HasPrefix(deviation, s.prefix) && len(s.prefix) > bestLen {
			best, bestLen = s.advice, len(s.prefix)
		}
	}
	return best
}

// Suggestions returns the distinct advice for a deviation list, in the order
// the matching deviations first appeared. Repeating the same advice once per
// deviation would bury the list it is meant to explain.
func Suggestions(deviations []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range deviations {
		advice := SuggestFor(d)
		if advice == "" || seen[advice] {
			continue
		}
		seen[advice] = true
		out = append(out, advice)
	}
	return out
}
