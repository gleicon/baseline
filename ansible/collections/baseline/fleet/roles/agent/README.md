# baseline.fleet.agent role

Installs baseline-agent (drift checker + OTel emitter), records the drift
manifest as the last step (via the agent binary `-write-manifest`), and
verifies the baseline reads clean.
No hub, no secrets, no certificates.

> **Golden rule:** baseline checks only what the manifest documents. Packages and
> services are snapshotted automatically; files are never snapshotted, so list
> each file in `baseline_manifest_files` or let `-scan-playbook` derive it. If it
> matters, promise it.

## Install

```sh
ansible-galaxy collection install \
  git+https://github.com/gleicon/baseline.git#/ansible/collections/baseline/fleet
```

Or vendor into your project:

```sh
mkdir -p collections/ansible_collections
cp -r /path/to/baseline/ansible/collections/baseline collections/ansible_collections/
```

```ini
# ansible.cfg
[defaults]
collections_path = ./collections
```

## Usage

```yaml
# site.yml — your existing provisioning playbook
- name: Provision web servers
  hosts: webservers
  become: true
  roles:
    - baseline.fleet.agent   # installs baseline-agent (copy or download), records + verifies manifest

  vars:
    baseline_release_base: "https://github.com/gleicon/baseline/releases/latest/download"
    baseline_otlp_endpoint: "http://otelcol.internal:4318"  # omit for stdout export
    baseline_manifest_files:
      - /etc/nginx/nginx.conf
      - /etc/ssh/sshd_config
    baseline_manifest_packages:
      - nginx
      - openssh-server
    baseline_manifest_services:
      - nginx
      - ssh

  tasks:
    # ... your existing tasks ...
    - name: Install nginx
      ansible.builtin.apt:
        name: nginx
        state: present

    - name: Deploy nginx config
      ansible.builtin.copy:
        src: nginx.conf
        dest: /etc/nginx/nginx.conf
        mode: "0644"
```

## Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `baseline_agent_binary` | optional | Local path to a pre-staged `baseline-agent` binary (copy) |
| `baseline_release_base` | optional | Download prefix; appends `/baseline-agent-linux-<arch>`. Set one of these two. |
| `baseline_otlp_endpoint` | unset (stdout) | OTLP/HTTP receiver for verdicts + liveness |
| `baseline_manifest_path` | `/etc/baseline/manifest.json` | Where the manifest is recorded |
| `baseline_manage_manifest` | `true` | Record + verify the manifest in the role (disable when your own tasks do it) |
| `baseline_manifest_snapshot` | `false` | Also record full package/service lists; later additions flag `pkg:unauthorized:*` / `svc:unauthorized:*` |
| `baseline_manifest_files` | `[]` | Absolute file paths to track |
| `baseline_manifest_packages` | `[]` | Package names to track |
| `baseline_manifest_services` | `[]` | Service names to track |
| `baseline_heartbeat_interval_sec` | `60` | Liveness emission interval (`baseline.alive`) |
| `baseline_audit_interval_sec` | `1200` | Manifest check interval |
| `baseline_audit_jitter_max_sec` | `120` | Splay jitter cap |

## Alerting (your OTel backend)

| Signal | Meaning | Suggested alert |
|--------|---------|-----------------|
| absent `baseline.alive{host.name}` | Node silent | dead-man / missing-series alert |
| `baseline.drift.status == 1` | Drift detected (deviations in the log record) | page / ticket |
| `baseline.drift.status == 2` | Check error (manifest missing) | provisioning follow-up |
