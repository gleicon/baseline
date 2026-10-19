# Baseline

Configuration drift detection for Ansible-managed servers. `baseline-agent` records
what a playbook provisions (files, packages, services) in a manifest and checks
the node against it on an interval, reporting verdicts and liveness over
OpenTelemetry. No hub, no secrets.

## Quick start

```sh
# record the baseline after provisioning
baseline-agent -write-manifest -manifest /etc/baseline/manifest.json \
  -files /etc/nginx/nginx.conf -packages nginx -services nginx -snapshot

# check for drift
baseline-agent -check -manifest /etc/baseline/manifest.json   # exit 0 clean, 1 drift, 2 error
```

Run the record step as the last task in your play. The `baseline.fleet.agent` role
does this via its `baseline_manifest_*` variables; directly it looks like:

```yaml
- name: Update baseline drift manifest
  ansible.builtin.command:
    argv: [baseline-agent, -write-manifest, -manifest, /etc/baseline/manifest.json,
           -files, /etc/nginx/nginx.conf,/etc/ssh/sshd_config, -snapshot]
```

Golden rule: baseline checks only what the manifest documents — an unlisted file
is an unchecked file. Full text and the reasoning:
[docs/drift-operations.md](docs/drift-operations.md#golden-rule).

The agent runs as a systemd daemon (`-config /etc/baseline/agent.toml`, only
`manifest_path` required) or one-shot (`-check`). Each check emits
`baseline.alive`, `baseline.drift.status` (0/1/2), `baseline.drift.details`, and
`baseline.checks.total` to stdout or OTLP/HTTP, plus a log record of the
deviations.

## Docs

| | |
|-|-|
| [docs/development.md](docs/development.md) | Flags, config, OTel signals, E2E |
| [docs/run-interception.md](docs/run-interception.md) | Record what any provisioning run changes; no recipe parsing |
| [docs/drift-operations.md](docs/drift-operations.md) | Find and fix drift, fleet queries, manifest signing |
| [docs/architecture.md](docs/architecture.md) | Components and data flows |
| [docs/otel-prometheus-grafana.md](docs/otel-prometheus-grafana.md) | Live observability stack and how to build your own |
| [Role README](ansible/collections/baseline/fleet/roles/agent/README.md) | Ansible install and variables |

Local E2E over Docker: `make e2e`, `make e2e-ansible`, `make e2e-systemd`,
`make e2e-live` (tear down with `make e2e-live-down`).

## License

MIT — see [LICENSE](LICENSE).
