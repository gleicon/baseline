# Baseline drift operations

Find out what drifted, fix it, and verify the fix, for one node or a fleet.

Default posture: detect and alert. Nothing is auto-fixed. The standard fix path
is Infrastructure as Code: re-run your provisioning playbook. Remediation is an
explicit opt-in (`-remediate`, one-shot, as root), never the default and never a
daemon setting.

## Golden rule

Baseline checks only what the manifest documents. Packages and services are
snapshotted automatically (anything extra flags as `pkg:unauthorized` /
`svc:unauthorized`). Files are never snapshotted: document each file you want
checked in Ansible (a `copy:`/`template:`/`lineinfile:` task, derived by
`-scan-playbook`) or directly in the manifest (`-write-manifest -files ...`,
or the role's `baseline_manifest_files`). If it matters, promise it.

## Find what drifted

The detail lives in the `baseline.drift.details` metric, which the agent emits only
while drifting, with the deviation list as the `deviations` label.

| Where | What you see |
|-------|--------------|
| Grafana dashboard *Baseline, drift detection* | *Drifted nodes (host, deviations)* table, per drifted node |
| Prometheus query | `baseline_drift_details` and `baseline_drift_status`, `ALERTS` |

Fleet queries:

```promql
# every drifted node and what is wrong with it
baseline_drift_details
# nodes with vim drift, without logging in
baseline_drift_details{deviations=~".*vim.*"}
# count of drifted nodes
count(baseline_drift_details)
```

Per node, from the shell:

```sh
baseline-agent -check -manifest /etc/baseline/manifest.json
baseline-agent -check -format json -manifest /etc/baseline/manifest.json
```

## Deviation legend

| Deviation | Meaning |
|-----------|---------|
| `file:hash_mismatch:<path>` | file content changed |
| `file:missing:<path>` | tracked file was deleted |
| `file:mode_mismatch:<path>` | permissions differ |
| `file:is_directory:<path>` | tracked file is now a directory |
| `file:size_mismatch:<path>` | size differs (check "size") |
| `file:mtime_mismatch:<path>` | modification time differs (check "mtime") |
| `pkg:missing:<name>` | declared package not installed |
| `pkg:version_mismatch:<name>:<have>:<want>` | wrong version |
| `pkg:unauthorized:<name>` | installed but not in the snapshot |
| `svc:inactive:<name>` / `svc:disabled:<name>` | declared service stopped / not enabled |
| `svc:unauthorized:<name>` | running service not in the snapshot |
| `svc:unverifiable:<name>` | systemd cannot be queried (D-Bus denied); grant access or run as root |
| `setting:mismatch:<key>:<have>:<want>` | live runtime value differs from the record (track via `-settings sysctl:<name>`) |
| `setting:unavailable:<key>` | runtime key cannot be queried |

## Fix

Re-run the provisioning playbook. It re-applies `template:`/`copy:`/`lineinfile:`
content (so deleted files come back), service states, and re-records the
manifest as the new baseline.

> **Re-recording adopts whatever is on the node right now.** The manifest is
> rewritten from live state, so every deviation present at that moment becomes
> the new promise — the one you were investigating included. If you installed
> vim by hand and re-run the play, vim becomes authorized and stops alerting.
> Re-run only after deciding the current state is correct.

Manual, single node:

```sh
# package you added: remove it, or keep it and adopt it via IaS
apt-get purge -y vim && apt-get autoremove -y --purge        # dnf remove -y vim

# deleted tracked file: restore from the playbook's source
ansible-playbook ...   # or copy the file back from VCS

# service state: re-match the manifest
systemctl start --now nginx && systemctl enable nginx        # or stop/disable
```

### Authorized auto-remediation (opt-in, off by default)

> Warning: `-remediate` enforces only the packages and
> services recorded in the manifest snapshot. The manifest is not a complete
> system-state model. Baseline does not know about and will not touch users,
> sudoers, SSH keys, cron jobs, firewall rules, sysctl parameters, or any other
> state Ansible manages. Re-running the provisioning playbook is the only
> complete remediation. Treat `-remediate` as a package/service tidy-up, never
> as a substitute for IaS.

```sh
# removes packages/services not in the snapshot, fixes declared service states.
# File deviations are still reported; only the playbook can restore content.
baseline-agent -check -remediate -manifest /etc/baseline/manifest.json
```

There is no daemon equivalent. The agent's audit loop never remediates: the
systemd unit runs it as an unprivileged user with no capabilities, so package
and service enforcement cannot work from there. Remediation is always an
explicit, root-invoked, one-shot command.

Example flow:

```sh
# operator installed vim on a node by hand
# 1. alert fires; Grafana *Drifted nodes* shows pkg:unauthorized:vim (+ deps)
# 2. decide: vim was a mistake
baseline-agent -check -remediate -manifest /etc/baseline/manifest.json
#    remediate: removed 2 package(s): vim, vim-common
# 3. verify
baseline-agent -check -manifest /etc/baseline/manifest.json      # OK: no drift
# alert auto-resolves, status 0, history kept
```

Fleet-wide, run `-remediate` via `ansible -m command` (as root). Grafana and
Prometheus show every node's deviations until it is back in compliance.