#!/usr/bin/env bash
# Start the local webhook receiver so Grafana alert notifications are visible.
# When BaselineDriftDetected fires, Grafana POSTs the alert here and it prints.
# Ctrl+C to stop. Log file: /tmp/baseline-webhook.log
set -euo pipefail
E2E_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec python3 "$E2E_DIR/webhook-listener.py" "${PORT:-9999}"