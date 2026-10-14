# baseline.fleet

Ansible collection for [Baseline](https://github.com/gleicon/baseline),
configuration drift detection for Ansible-managed servers. No hub, no secrets.

| Content | Purpose |
|---------|---------|
| [`agent` role](roles/agent/README.md) | Installs `baseline-agent`, writes the systemd unit, records the drift manifest as the last provisioning step, and verifies it reads clean |
| `baseline_capture` callback | Controller-side capture of every file a run touched, for nodes you cannot wrap with the record tasks |

## Install

```sh
ansible-galaxy collection install \
  git+https://github.com/gleicon/baseline.git#/ansible/collections/baseline/fleet
```

Or vendor it:

```sh
mkdir -p collections/ansible_collections
cp -r /path/to/baseline/ansible/collections/baseline collections/ansible_collections/
```

```ini
# ansible.cfg
[defaults]
collections_path = ./collections
```

## Golden rule

Baseline checks only what the manifest documents. Packages and services are
snapshotted automatically, so anything added later flags as
`pkg:unauthorized:*` / `svc:unauthorized:*`. **Files are never snapshotted**:
every file you want checked must be listed in `baseline_manifest_files` (or
written into the manifest directly). An unlisted file is an unchecked file.

If it matters, promise it.

## Usage

See the [`agent` role README](roles/agent/README.md) for the variable reference
and a worked example. Remediation is not a daemon setting — run it one-shot, as
root, when you have decided the current state is wrong:

```sh
baseline-agent -check -remediate -manifest /etc/baseline/manifest.json
```

The complete fix for drift is always re-running the provisioning playbook.
