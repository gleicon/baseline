# Baseline — resolved decisions

Interview record from `/ds-grill-me`. Each entry is a decision that is hard to
reverse, surprising without context, or the product of a real trade-off.

## 1. Daemon-mode remediation is removed, not fixed

**Question:** `remediate = true` in `agent.toml` is honored by the daemon
(`cmd/agent/main.go:223`), but the systemd unit the role installs runs the agent
as `User=baseline` with `CapabilityBoundingSet=` empty and `NoNewPrivileges=true`.
The `apt-get` and `systemctl` calls in `internal/agent/audit/remediate_linux.go`
discard their error returns, so under that unit remediation fails silently and
reports nothing. Document it as unsupported, give the unit the privileges to make
it work, or remove the capability?

**Decision:** Remove it. Delete the `cfg.Remediate` branch in the audit loop and
the `remediate` key from `AgentConfig` / `agentSection`. The one-shot
`-remediate` flag is unchanged.

**Rationale:** The one-shot path (`baseline-agent -check -remediate`, invoked by
Ansible as root) is the only remediation path with e2e coverage
(`e2e/ansible/drift.yml:86-104`). Making the daemon path work would require a
root-capable auto-remover resident on every node — a much larger blast radius
than the feature justifies. Removing it eliminates a silently-broken feature
rather than documenting one. Accepted cost: a breaking config change for anyone
who already set `remediate = true`.

## 2. The config loader warns on keys it does not know

**Question:** `internal/agent/config/config.go:60` discards the `MetaData` from
`toml.Decode`, and BurntSushi/toml does not treat unknown keys as errors. Once
`remediate` leaves the struct (decision 1), a node whose `agent.toml` still sets
`remediate = true` parses clean and the operator keeps believing enforcement is
on. Warn, hard-error, or keep ignoring?

**Decision:** Capture the `MetaData` and `slog.Warn` any undecoded keys at
startup, naming the keys and the config path. The node still starts.

**Rationale:** A hard error makes a stale key fatal, and a fatal config stops
`baseline.alive`, which makes `BaselineNodeSilent` fire — a config typo would
become a fleet-wide dead-man alert storm on upgrade. Warning puts the message
where the operator already looks (the service log) without turning a
documentation problem into an availability problem. It also catches typos in
every other key for free, which a retired-key list would not.

## 3. Remediation reports only what demonstrably succeeded

**Question:** `internal/agent/audit/remediate_linux.go` uses three different
fidelity strategies in one function: packages are reconciled against post-run
state, declared services check the helper's error return, and unauthorized
services check nothing — `stopService` and `disableService` both return `error`,
both are discarded, and the name is appended to `rem.ServicesStopped` regardless.
On a `static` or masked unit the stop succeeds, the disable fails, and the
operator reads "stopped 1 service(s)" for a service that returns at next boot.

**Decision:** Append to `ServicesStopped` only when `stopService` returns nil,
matching the declared-services branch directly below it.

**Rationale:** One fidelity rule for the whole function. A still-failing disable
re-flags as `svc:unauthorized` on the next check, so the deviation is not lost —
which is why an explicit `Failed` field on `Remediation` was not worth widening
the struct and the output format for.

## 4. The Golden rule has two homes, not one or four

**Question:** The Golden rule is verbatim in `README.md`, `docs/development.md`,
`docs/drift-operations.md`, and the role README, and has already diverged — the
role README's copy tells the reader to let `-scan-playbook` derive the file list,
which the role never invokes.

**Decision:** `docs/drift-operations.md` holds the canonical text. `README.md`
and `docs/development.md` shrink to one line plus a link. The role README keeps
its own full copy, with the `-scan-playbook` mention removed.

**Rationale:** The role ships standalone through
`ansible-galaxy collection install git+...`, so its reader has no `docs/` tree
reachable — a relative link would be dead at exactly the moment the rule matters,
when they are deciding what goes in `baseline_manifest_files`. Two maintained
copies, each with a reader who cannot see the other.

## 5. The collection gets `authors` and a real collection README

**Question:** `galaxy.yml` declares `readme: README.md`, which does not exist at
the collection root, and omits `authors`.

**Decision:** Add `authors: [Gleicon Moraes]`. Write a thin
`ansible/collections/baseline/fleet/README.md` covering what the collection ships
(the `agent` role, the `baseline_capture` callback), the install command, the
Golden rule in full, and a pointer to `roles/agent/README.md` for variables.

**Rationale:** `authors` is not cosmetic — `ansible-galaxy` treats it as
mandatory, so both `collection build` and the install command documented in the
role README abort today with "missing the following mandatory keys: authors".
Verified by building a scratch copy: adding `authors` alone makes the build
succeed, and a `readme:` pointing at a missing file is tolerated. The README is
still worth writing because a collection published to Galaxy renders the
collection-root README as its landing page, and that is where a new reader meets
the Golden rule.

## 6. The role README links up rather than carrying its own copy

**Question:** Decision 4 gave the role README a standalone copy of the Golden
rule because its reader has no `docs/` tree. Decision 5 adds a collection README
that also carries the rule in full. Three copies, or fold one back?

**Decision:** Full text in `docs/drift-operations.md` (canonical for operators)
and in the collection README (standalone for collection consumers). The role
README carries one line plus a link to `../../README.md`.

**Rationale:** The link resolves both in-repo and inside the installed tarball,
so the standalone reader can follow it — which removes the justification decision
4 rested on. Two maintained copies instead of three; at four copies this rule had
already diverged once.

## 7. The observability stack is documented as a demo, not a template

**Question:** `docs/otel-prometheus-grafana.md` frames `e2e/observe` as
"configuration you can copy to build your own monitoring environment". As shipped
it has anonymous Grafana read access (`GF_AUTH_ANONYMOUS_ENABLED: "true"`,
`ORG_ROLE: Viewer`), `admin`/`admin`, `--storage.tsdb.retention.time=2h`, and all
ports on `0.0.0.0` with no TLS. Harden the stack, or change the framing?

**Decision:** Leave the stack alone and fix the framing. Add a "not
production-safe as shipped" block to "Build your own environment" naming all four
properties, with a shipped-vs-real table.

**Rationale:** Those settings are correct for a 60-second e2e run; hardening them
would complicate the suite to serve a reader who is copying, not running.
Retention is the one that most needs saying out loud: it is set on Prometheus's
command line in `docker-compose.observe.yml`, not in the `prometheus.yml` the doc
tells the reader to copy, so following the steps faithfully yields a 2-hour-deep
drift history with nothing to hint at why.

## 8. CI publishes release binaries; the download path stays documented

**Question:** The role README's only usage example sets
`baseline_release_base: "https://github.com/gleicon/baseline/releases/latest/download"`,
but `gh release view` reports zero assets on every tag and `ci.yml` has no
release job, so `tasks/main.yml:53-63` `get_url` 404s. Every path with e2e
coverage uses `baseline_agent_binary` instead
(`e2e/systemd/provision.yml:15`, `e2e/ansible/run.sh:73`). Build the artifacts,
or demote the variable the way daemon remediation was demoted in decision 1?

**Decision:** Add `.github/workflows/release.yml` building linux amd64 and arm64
on tag push and uploading them as `baseline-agent-linux-<arch>` — the exact names
`tasks/main.yml:55` already constructs. Role, README, and defaults stay as
written.

**Rationale:** Decision 1's precedent (prefer the tested path) was weighed and
set aside here: unlike daemon remediation, the download path is not
silently-broken-by-design but merely unbuilt, and a fleet install wants a URL
rather than a pre-staged file on every controller. The gap is one workflow file,
not a capability.

## 9. Existing releases are backfilled by manual dispatch, not a re-tag

**Question:** A `tags: ["v*"]` trigger only fires on new tags, so
`releases/latest/download` stays a 404 against `v0.3.0` after the workflow lands.

**Decision:** Give the workflow a `workflow_dispatch` trigger with a tag input
alongside the push trigger, and run it once against `v0.3.0`.

**Rationale:** Deleting and re-pushing `v0.3.0` would fire the push trigger, but
it rewrites a published Git ref — pinned consumers see the tag move and
`releases/latest` briefly vanishes — which is far too much blast radius for
backfilling build artifacts. The manual trigger costs four lines, moves no refs,
makes the documented URL work immediately, and stays useful for re-running a
failed upload. `galaxy.yml:3` is also stale at `version: 0.1.0` against tag
`v0.3.0`; version bumping belongs in the same release routine.

## 10. Snapshot noise is a documentation problem, not a walker feature

**Question:** `BuildFileIndex` (`internal/agent/audit/record.go:28-54`) has no
exclusion list — it skips directories, non-regular files and unreadable subtrees,
nothing else. The day-one bootstrap the docs recommend
(`-record-snapshot -dirs /etc,...`) therefore tracks machine-churned files:
`resolv.conf` (DHCP), `ld.so.cache` (any package install), `adjtime` (clock
sync), `shadow` (password change). Each becomes a permanent `file:hash_mismatch`
with no playbook-shaped fix. Ship a default skip list, add an `-exclude` flag, or
fix the guidance?

**Decision:** No code change. Keep the walker policy-free. Change the bootstrap
guidance in `docs/run-interception.md` to recommend service config directories
(`/etc/nginx`, `/etc/ssh`, `/opt/app`) instead of all of `/etc`, and name the
churning files so an operator recognises the symptom.

**Rationale:** A built-in skip list puts policy in the walker, needs maintaining,
and would silently stop tracking `/etc/shadow` — a file some operators
specifically want watched. An `-exclude` flag adds a fourth record-mode flag and
still needs the same guidance to be useful. The actual defect is that the docs
recommended the noisiest possible configuration as the first thing a new user
runs.

---

# Implementation

All ten decisions are implemented in the working tree. `go build`, `go vet` and
`go test -race` pass; the collection builds clean with `ansible-galaxy
collection build`.

Shipped as **v0.4.0**. A minor bump rather than a patch because removing the
`remediate` config key is a breaking change, and rather than a major because the
project is pre-1.0.

Pushing the tag publishes the binaries through `release.yml`, which makes
`releases/latest/download` resolve and `baseline_release_base` work for the first
time. That removes the need for the decision-9 backfill on the *latest* release;
`workflow_dispatch` remains available if the older tags should also carry assets:

```sh
gh workflow run release.yml -f tag=v0.3.0
```

One correction to the workflow as first drafted: `gh release upload` requires the
GitHub release to already exist, and a bare tag push creates only a Git ref. The
workflow now creates the release when absent, and runs as a single job because a
per-arch matrix would race on that creation.

---

# Mechanical follow-ups (all applied)

Consequences of the above, recorded so they are not lost.

**From decision 1 (daemon remediation removed):**
- Delete the `cfg.Remediate` branch at `cmd/agent/main.go:220-228`.
- Drop `Remediate` from `AgentConfig` and `agentSection` in
  `internal/agent/config/config.go`.
- Remove the warning block from
  `ansible/collections/baseline/fleet/roles/agent/templates/agent.toml.j2:10-16`
  (and with it the never-declared `baseline_remediate` variable it referenced).
- `docs/drift-operations.md`: cut the "Daemon-mode enforcement on every audit"
  section; drop `remediate = true` from the posture statement at `:5-7` and from
  the fleet-wide advice at `:134`.
- `docs/development.md`: drop the `remediate` config comment at `:97-98`.
- `.project/PROJECT.md`: drop `remediate = true` from the overview at `:11-13`
  and from the `internal/agent/config/` row at `:35`.
- `internal/agent/audit/manifest.go:33` and `remediate_linux.go:28`: comments
  cite `remediate=true`; update to the flag only.

**Doc fixes still outstanding (from the quality review):**
- `docs/drift-operations.md`: cut `## Promises`; its surviving payload is the
  "re-recording adopts the current state" warning, which should become a callout
  in `## Fix` rather than a bullet.
- `docs/development.md`: state the `fast_scan` failure mode (a content change
  preserving size and mtime goes undetected) and that `-snapshot` flags additions
  relative to record time.
- `docs/run-interception.md`: state that the `-record-*` modes need root; promote
  the "node inventory" definition out of the `## Principle` paragraph.
- `docs/otel-prometheus-grafana.md`: redraw or replace the ASCII diagram at
  `:14-21` (stray "git", arms do not align with their sources).
