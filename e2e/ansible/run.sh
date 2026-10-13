#!/usr/bin/env bash
# Baseline E2E driven by Ansible (open-source/free ansible-playbook) against a
# throwaway Docker container — nothing on the host or any VM is modified.
#
#   stage 0  -scan-playbook on the host (read-only): derived paths must match bootstrap.yml
#   stage 1  ansible-playbook bootstrap.yml -> install agent, baseline, snapshot manifest, clean check
#   stage 2  ansible-playbook drift.yml     -> corrupt sentinel + install curl, assert drift, restore, assert clean
#   cleanup  container removed + asserted gone
#
# Usage: ./e2e/ansible/run.sh   (from the repo root)  |  make e2e-ansible
set -euo pipefail

E2E_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$E2E_DIR/../.." && pwd)"
BIN_DIR="$E2E_DIR/bin"
BASELINE="$REPO_ROOT/e2e/baseline/sentinel.txt"
CTR="baseline-e2e-ansible"

require() { command -v "$1" >/dev/null 2>&1 || { echo "ERROR: $1 not found"; exit 1; }; }

require docker
require ansible-playbook

if ! ansible-galaxy collection list 2>/dev/null | grep -q 'community\.docker'; then
  echo "installing community.docker collection..."
  ansible-galaxy collection install community.docker
fi

# Expose the source baseline.fleet collection to Ansible via the standard
# <path>/ansible_collections/<ns>/<name> layout (symlink, repo-local, gitignored).
# Set BOTH env vars: ansible-core 2.21 maps ANSIBLE_COLLECTIONS_PATH (singular),
# older cores map the plural ANSIBLE_COLLECTIONS_PATHS. One of them is always inert.
COLL_STAGE="$E2E_DIR/collections"
mkdir -p "$COLL_STAGE/ansible_collections/baseline"
ln -sfn "$REPO_ROOT/ansible/collections/baseline/fleet" "$COLL_STAGE/ansible_collections/baseline/fleet"
export ANSIBLE_COLLECTIONS_PATH="$COLL_STAGE"
export ANSIBLE_COLLECTIONS_PATHS="$COLL_STAGE"

# Target the Docker daemon's platform (the Colima VM here is aarch64).
case "$(docker info --format '{{.Architecture}}' 2>/dev/null)" in
  x86_64 | amd64) GOARCH=amd64 ;;
  aarch64 | arm64) GOARCH=arm64 ;;
  *) GOARCH=amd64 ;;
esac

echo "=== build linux/$GOARCH agent (repo-local, gitignored) ==="
mkdir -p "$BIN_DIR"
(cd "$REPO_ROOT" && GOOS=linux GOARCH="$GOARCH" CGO_ENABLED=0 go build -o "$BIN_DIR/baseline-agent" ./cmd/agent)

echo ""
echo "=== stage 0: -scan-playbook on the host (read-only) ==="
scanned="$(cd "$REPO_ROOT" && go run ./cmd/agent -scan-playbook "$E2E_DIR/scan-source.yml" 2>/dev/null)"
if echo "$scanned" | grep -q '/etc/baseline/sentinel' && echo "$scanned" | grep -q '/etc/motd'; then
  echo "PASS: scan-playbook derives /etc/baseline/sentinel and /etc/motd"
else
  echo "FAIL: scan-playbook output missing expected paths:"
  echo "$scanned"
  exit 1
fi

echo "=== start throwaway container $CTR ==="
docker rm -f "$CTR" >/dev/null 2>&1 || true
docker run -d --privileged --name "$CTR" ubuntu:24.04 sleep infinity >/dev/null

echo "=== install python3 + python3-apt in container (Ansible interpreter / apt module) ==="
docker exec "$CTR" bash -c \
  "apt-get update -q && apt-get install -y -q --no-install-recommends python3 python3-apt" >/dev/null

ANSIBLE_ARGS=(
  -i "$CTR,"
  -e "ansible_connection=community.docker.docker"
  -e "ansible_python_interpreter=/usr/bin/python3"
  -e "baseline_agent_binary=$BIN_DIR/baseline-agent"
  -e "baseline_file=$BASELINE"
)

finish() {
  # Explicit cleanup that also asserts the container is gone.
  docker rm -f "$CTR" >/dev/null 2>&1 || true
  rm -rf "$COLL_STAGE"
  if docker ps -a --format '{{.Names}}' | grep -q "^$CTR$"; then
    echo "FAIL: container $CTR not removed"
    exit 1
  fi
}
trap finish EXIT

echo ""
echo "=== stage 1: provision baseline via Ansible ==="
ansible-playbook "${ANSIBLE_ARGS[@]}" "$E2E_DIR/bootstrap.yml"
# Gate: fail if the play silently matched no hosts (ansible-playbook exits 0
# even when everything is skipped).
docker exec "$CTR" test -x /usr/local/bin/baseline-agent
docker exec "$CTR" test -f /etc/baseline/manifest.json

echo ""
echo "=== stage 1b: scan-driven manifest + check (module↔Go equivalence via scan) ==="
docker cp "$E2E_DIR/scan-source.yml" "$CTR:/tmp/scan-source.yml"
docker exec "$CTR" /usr/local/bin/baseline-agent \
  -write-manifest -manifest /etc/baseline/scan-manifest.json -scan-playbook /tmp/scan-source.yml
docker exec "$CTR" /usr/local/bin/baseline-agent -check -manifest /etc/baseline/scan-manifest.json \
  | grep -q "OK" && echo "PASS: scan→manifest→check reports clean"

echo ""
echo "=== stage 1c: record interception (before/after diff = ansible inventory) ==="
docker exec -i "$CTR" bash -e << 'EOF'
set -e
rm -rf /tmp/rec && mkdir -p /tmp/rec/conf
printf 'a=1\n' > /tmp/rec/conf/a.conf
printf 'c=1\n' > /tmp/rec/conf/c.conf
baseline-agent -record-begin -data /tmp/rec/pre.json -dirs /tmp/rec/conf \
  | grep -q "recorded 2 files"
printf 'a=2\n' > /tmp/rec/conf/a.conf     # changed
printf 'b=3\n' > /tmp/rec/conf/b.conf     # added
rm /tmp/rec/conf/c.conf                   # deleted
baseline-agent -record-end -manifest /tmp/rec/manifest.json -data /tmp/rec/pre.json -dirs /tmp/rec/conf \
  | grep -q "files added=2 removed=1"
baseline-agent -check -manifest /tmp/rec/manifest.json | grep -q "OK: no drift"
printf 'tampered\n' > /tmp/rec/conf/a.conf
baseline-agent -check -manifest /tmp/rec/manifest.json | grep -q "DRIFT"
echo "PASS: record-begin/end derive the node inventory and detect drift"
EOF

echo ""
echo "=== stage 1d: dynamic settings (live sysctl value) ==="
docker exec "$CTR" sh -c '
set -e
sysctl -w net.ipv4.ip_forward=0 >/dev/null
baseline-agent -write-manifest -manifest /tmp/settings.json -settings sysctl:net.ipv4.ip_forward \
  | grep -q "settings=1"
baseline-agent -check -manifest /tmp/settings.json | grep -q "OK: no drift"
sysctl -w net.ipv4.ip_forward=1 >/dev/null
baseline-agent -check -manifest /tmp/settings.json | grep -q "setting:mismatch:sysctl:net.ipv4.ip_forward:1:0"
sysctl -w net.ipv4.ip_forward=0 >/dev/null
baseline-agent -check -manifest /tmp/settings.json | grep -q "OK: no drift"
echo "PASS: settings section detects a live sysctl change"

echo "--- module digest roundtrip ---"
baseline-agent -write-manifest -manifest /tmp/modules.json -settings modules \
  | grep -q "settings=1"
baseline-agent -check -manifest /tmp/modules.json | grep -q "OK: no drift"
echo "PASS: modules backend records a stable digest and checks clean"

echo "--- sysctl:all snapshot with prefix ---"
baseline-agent -write-manifest -manifest /tmp/sysctl-all.json -settings "sysctl:all:net.ipv4.*" \
  | grep -q "settings=1"
baseline-agent -check -manifest /tmp/sysctl-all.json | grep -q "OK: no drift"
sysctl -w net.ipv4.ip_forward=1 >/dev/null
baseline-agent -check -manifest /tmp/sysctl-all.json \
  | grep -q "setting:mismatch:sysctl:net.ipv4.ip_forward"
sysctl -w net.ipv4.ip_forward=0 >/dev/null
baseline-agent -check -manifest /tmp/sysctl-all.json | grep -q "OK: no drift"
echo "PASS: sysctl:all snapshot reports the changed key"
'

echo ""
echo "=== stage 1e: record-snapshot baselines an existing tree (no diff) ==="
docker exec "$CTR" sh -c '
set -e
rm -rf /tmp/snap && mkdir -p /tmp/snap/etc
printf "x=1\n" > /tmp/snap/etc/x.conf
baseline-agent -record-snapshot -manifest /tmp/snap/manifest.json -dirs /tmp/snap/etc \
  | grep -q "files=1"
baseline-agent -check -manifest /tmp/snap/manifest.json | grep -q "OK: no drift"
printf "x=2\n" > /tmp/snap/etc/x.conf
baseline-agent -check -manifest /tmp/snap/manifest.json | grep -q "DRIFT"
echo "PASS: record-snapshot baselines the current tree and detects drift"
'

echo ""
echo "=== stage 1f: controller-side capture (baseline_capture callback) ==="
OBSDIR="$REPO_ROOT/e2e/out/observed"
rm -rf "$OBSDIR" && mkdir -p "$OBSDIR"
ANSIBLE_CALLBACK_PLUGINS="$REPO_ROOT/ansible/collections/baseline/fleet/plugins/callback" \
ANSIBLE_CALLBACKS_ENABLED=baseline_capture \
BASELINE_OBSERVED_DIR="$OBSDIR" \
  ansible-playbook "${ANSIBLE_ARGS[@]}" "$E2E_DIR/capture.yml" >/dev/null
grep -q '"sha256"' "$OBSDIR/baseline-e2e-ansible.json" \
  && grep -q '/tmp/capture.conf' "$OBSDIR/baseline-e2e-ansible.json" \
  && echo "PASS: callback wrote an observed set on the controller"
docker cp "$OBSDIR/baseline-e2e-ansible.json" "$CTR:/tmp/observed.json"
docker exec "$CTR" sh -c '
set -e
baseline-agent -record-observed -manifest /tmp/observed-manifest.json -data /tmp/observed.json \
  | grep -q "files=2"
baseline-agent -check -manifest /tmp/observed-manifest.json | grep -q "OK: no drift"
echo "tampered" >> /tmp/capture.conf
baseline-agent -check -manifest /tmp/observed-manifest.json | grep -q "DRIFT"
echo "PASS: -record-observed builds a checkable manifest and detects drift"
'

echo ""
echo "=== stage 2: introduce drift, verify detection, restore ==="
ansible-playbook "${ANSIBLE_ARGS[@]}" "$E2E_DIR/drift.yml"

echo ""
echo "E2E (Ansible over Docker): all stages passed"