# Baseline drift operations

Find out what drifted, fix it, and verify the fix, for one node or a fleet.

Baseline detects and reports. It never changes the node — not the files, not
the packages, not the services. Every check names what deviated and suggests
the options; the decision and the fix are yours, and the fix path is
Infrastructure as Code: re-run your provisioning playbook.

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

Example flow:

```sh
# operator installed vim on a node by hand
# 1. alert fires; Grafana *Drifted nodes* shows pkg:unauthorized:vim (+ deps)
# 2. the check names the options:
baseline-agent -check -manifest /etc/baseline/manifest.json
#    DRIFT: 2 deviation(s)
#      - pkg:unauthorized:vim
#      - pkg:unauthorized:vim-common
#    what to do:
#      · package installed outside the baseline — remove it with your package
#        manager, or re-run the playbook to adopt it as the new promise
# 3. decide: vim was a mistake. Remove it yourself, or re-run the play.
apt-get purge -y vim && apt-get autoremove -y --purge
# 4. verify
baseline-agent -check -manifest /etc/baseline/manifest.json      # OK: no drift
# alert auto-resolves, status 0, history kept
```

Grafana and Prometheus show every node's deviations until it is back in
compliance.

## Manifest signing

The manifest is the promise. If it can be edited, every verdict derived from it
is worthless — so the agent signs it at record time and verifies it on every
check.

```
/etc/baseline/manifest.json       the promise
/etc/baseline/manifest.json.sig   detached ed25519 signature (0444)
/etc/baseline/signing.key         private seed (0400), generated on first record
/etc/baseline/signing.pub         public key (0444)
```

A manifest edited without re-signing fails verification. The check reports
`manifest:signature_invalid:<path>` and exits 2 (`CHECK_ERROR`) rather than
reporting on a document it cannot trust.

```sh
baseline-agent -check -manifest /etc/baseline/manifest.json
# CHECK_ERROR
#   - manifest:signature_invalid:/etc/baseline/manifest.json
```

Re-recording the manifest re-signs it, so a normal provisioning run needs no
extra step.

### What signing does and does not buy you

> **Signing makes tampering evident, not impossible.** Nothing stored on a node
> can defeat root on that node. An attacker who can edit a tracked file can also
> edit the manifest, regenerate the signing key, and re-sign. Verification will
> pass. Do not treat a verified manifest as proof of integrity against a
> privileged adversary.

What it does buy, in order of how often it matters:

1. **Locally, for free:** a corrupted manifest, or a tracked file edited and the
   manifest doctored to match without re-signing, now fails loudly. That covers
   the realistic cases — careless change and accidental corruption.
2. **Off-node, through telemetry you already run:** every check emits
   `baseline.manifest.digest` (sha256 of the manifest) and
   `baseline.manifest.key_fp` (signing-key fingerprint), plus
   `baseline.manifest.signed` and `baseline.manifest.verified`. A fingerprint
   that changes without a deployment means the key was regenerated — visible in
   a backend the attacker on that node does not control. **That off-node
   witness, not the signature itself, is the security property.**

Alert on it:

```promql
# the signing key changed on a node — re-keyed, or re-signed by someone else
changes(baseline_drift_status{manifest_key_fp!=""}[1h]) > 0
```

Better: record the expected fingerprint per node at deploy time and alert on any
value that differs.

### Unsigned manifests

A manifest recorded before signing existed has no `.sig`. The agent still checks
it and reports `baseline.manifest.signed=false` rather than failing — an upgrade
must not silence a fleet. Re-record to start signing.