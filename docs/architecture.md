# Baseline architecture

One binary (`baseline-agent`) plus the `baseline.fleet` Ansible collection. No server
side. Verdicts and liveness go to your OpenTelemetry pipeline, where all
alerting happens. The agent binary is the single manifest generator; Ansible
invokes `-write-manifest` as the last task.

```
provision (ansible, plain SSH)              operate (OTel backend)
──────────────────────────────              ─────────────────────
baseline-agent -write-manifest (last task)
  → /etc/baseline/manifest.json          baseline-agent (systemd unit)
     │                                 ├─ heartbeat loop → baseline.alive
     │                                 ├─ audit loop → baseline.drift.status
     │                                 ├─   + baseline.drift.details (deviations)
     │                                 ├─   + remediate (opt-in)
     └─ promise ───────────────────→  └─ check log record (baseline.deviations)
                                              │  stdout (default) or OTLP/HTTP
                                              ▼
                                     collector → Grafana / Alertmanager
                                       ├─ absent baseline.alive → node silent
                                       └─ baseline.drift.status == 1 → drift
```

## Components

| Package | Responsibility |
|---------|----------------|
| `cmd/agent` | Flags, OTel setup, signal handling, systemd notify |
| `internal/agent/config` | `agent.toml` (only `manifest_path` required) |
| `internal/agent/audit` | Manifest read / check / generate / scan / remediate |
| `internal/agent/otel` | OTel providers; gauges, counter, log records |
| `internal/agent/sdnotify` | Inline sd_notify, no CGO |
| `internal/assert` | Precondition helper |
| `baseline.fleet` collection | `agent` role (provision-time) plus the `baseline_capture` callback plugin; both drive the agent binary |

## Rules

- `cmd/agent` imports only `internal/agent/*`
- `audit` never touches OTel; only `otel` imports the SDK
- Manifest modes: `declared` (listed items) / `snapshot` (adds full package and
  service lists)
- Verdicts: `OK` (0) / `DRIFT_DETECTED` (1) / `MANIFEST_NOT_FOUND` or
  `CHECK_ERROR` (2); a stale manifest (>90 days) warns without changing the
  verdict
- No new external dependencies without written justification

## Limits (systemd unit)

CPU 10%, memory 256M, IO 512K, `Nice=19`, watchdog 120s.