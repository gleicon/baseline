# Baseline development and usage

## Prerequisites

Go 1.26+, Ansible (`brew install ansible`), Docker. Lima/Colima/Multipass for
the VM dev loop (`brew install lima`); on Linux no VM is needed.

## Flags

| Flag | Meaning |
|------|---------|
| `-check` | One-shot manifest check, then exit (mutually exclusive with `-write-manifest`) |
| `-write-manifest` | Generate a manifest from live state, then exit |
| `-manifest PATH` | Check target, or `-write-manifest` destination |
| `-files a,b` | Absolute file paths to hash |
| `-packages a,b` | Package names (`name` or `name=version` to pin) |
| `-services a,b` | Service names (actual state is recorded) |
| `-settings a,b` | Runtime keys to track (`sysctl:<name>`); the live value is recorded and checked |
| `-snapshot` | Also record all installed packages and active services. Anything installed or started *after* this recording flags as `pkg:unauthorized:*` / `svc:unauthorized:*` |
| `-scan-playbook a.yml` | Extract managed paths from playbooks (feeds `-write-manifest`, or prints them alone) |
| `-format text\|json` | `-check` output (default `text`); exits 0 clean / 1 drift / 2 error |
| `-node-id ID` | Report label (default: hostname) |
| `-config PATH` | Agent TOML (`manifest_path` is the only required field) |
| `-otlp-endpoint URL` | OTLP/HTTP receiver (default: `OTEL_EXPORTER_OTLP_ENDPOINT` env, else stdout) |
| `-exclude a,b` | `record`: glob patterns to skip. Matches a full path or any parent directory, so `/etc/kubernetes/pki` drops that whole subtree. An invalid pattern is an error, never a silent no-match |
| `-record-begin` | Index the file tree under `-dirs` into `-data` before a change window |
| `-record-end` | Diff `-data` against a fresh index and merge the changed files into `-manifest` (the node inventory) |
| `-record-snapshot` | Track every file under `-dirs` as a baseline (no diff); bootstrap an already-provisioned node |
| `-record-observed` | Merge a controller-captured file set (`-data`, from the `baseline_capture` callback) into `-manifest` |
| `-data PATH` | Record index / observed set path |
| `-dirs a,b` | Record roots to index |

## Manifest sources

The agent binary produces the manifest three ways: as the last task in the
play (via `ansible -m command`, plain SSH, no daemon), from the CLI
(`-write-manifest`), or from the playbook scanner (`-scan-playbook`). They all
build the same file. Track what the playbook manages (`template:` / `copy:` /
`lineinfile:` destinations); that is the drift surface.

Golden rule: baseline checks only what the manifest documents — an unlisted file
is an unchecked file. Full text:
[drift-operations.md](drift-operations.md#golden-rule).

The scanner follows tasks, handlers, blocks, static includes, play `vars:` and
`vars_files:`, and static `loop:` over `{{ item }}`. What it cannot resolve
(templated paths, dynamic includes, `unarchive` members, `state=absent`) is
reported on stderr. Review the warnings.

Package and service checks use the native manager: dpkg/rpm + systemctl on
Linux, Homebrew on macOS. Generation and checking share the lookups.

## OTel signals

`baseline.alive` gauge (missing series = silent node), `baseline.drift.status` (0/1/2),
`baseline.drift.details` gauge while drifting (deviation list as the `deviations`
label, for a fleet view), `baseline.checks.total` counter, one log record per check
(`baseline.status`, `baseline.deviations[]`, `baseline.manifest_generated_at`).

## Dynamic settings

Track live runtime values with `settings`. Backends are hard-coded (no
arbitrary commands):

| Key | Records |
|-----|---------|
| `sysctl:<name>` | live value of a single kernel parameter |
| `sysctl:all` | every sysctl value, as a snapshot; a changed key reports per-key |
| `sysctl:all:<prefix>` | only sysctls under the prefix, e.g. `sysctl:all:net.ipv4.*` |
| `modules` | sha256 of the sorted loaded kernel module names (`/proc/modules`) |
| `bpf` | sha256 of loaded eBPF program counts by type (requires `bpftool`) |

```sh
baseline-agent -write-manifest -manifest /etc/baseline/manifest.json \
  -settings sysctl:net.ipv4.tcp_syncookies,modules -snapshot
```

`sysctl:all[:prefix]` snapshots the whole namespace (or a prefix of it) into the
manifest's `sysctl_snapshot`; the check runs `sysctl -a` once and reports a
`setting:mismatch:sysctl:<name>` per changed value. Use a prefix to skip the
sysctls that are auto-tuned by the kernel or other tools. Each check re-queries
the value; a divergence reports `setting:mismatch:...` and an unqueryable key
`setting:unavailable:...` (see [drift-operations.md](drift-operations.md)). The
`bpf` backend aggregates only program type counts, so transient load/unload
churn stays out of the promise unless a whole program type appears or
disappears.

## Config

```toml
[agent]
node_id               = "web-01"              # optional, defaults to hostname
manifest_path         = "/etc/baseline/manifest.json"
heartbeat_interval_sec = 60
audit_interval_sec    = 1200
audit_jitter_max_sec  = 120
# otlp_endpoint = "http://otelcol.internal:4318"  # omit for stdout export
# check         = ["sha256", "mode"]   # file attributes to compare; add "size"
#                                      # or "mtime" to check those too
# fast_scan     = false  # trust size+mtime and skip the sha256 for unchanged
#                        # files. ~6x faster checks on large inventories.
#                        # Trade-off: a content change that preserves both size
#                        # and mtime is NOT detected (a rewrite that restores
#                        # the timestamp). Leave it off where that matters.
```

## E2E over Docker

All suites run in Docker with no host or VM changes.

| Target | What it runs |
|--------|--------------|
| `make e2e` | Agent daemon to OTel collector; clean, file drift, package drift, restore |
| `make e2e-ansible` | Provision a throwaway container with Ansible (`-write-manifest`, snapshot), scan-playbook, record interception, settings (sysctl/modules/sysctl:all), drift, suggestions, manifest-signature tamper detection, restore |
| `make e2e-systemd` | The `baseline.fleet.agent` role in a real systemd container; service and file drift, permission alert |
| `make e2e-live` | Daemon to collector to Prometheus to Grafana; leaves the stack running (tear down with `make e2e-live-down`) |
| `make e2e-live-webhook` | Optional listener for the live stack; prints Grafana alert notifications as they fire |

The agent binary is the single manifest generator; Ansible invokes
`-write-manifest` directly (the `agent` role does this as its last step).

## Makefile

```
build test test-linux
e2e e2e-ansible e2e-systemd e2e-live e2e-live-down e2e-live-webhook
dev-init dev-setup dev-vm-create dev-vm-start dev-vm-stop dev-vm-status
dev-build dev-inventory dev-deploy
dev-run dev-logs dev-journal dev-watch dev-stop
dev-drift dev-restore dev-check-drift
```