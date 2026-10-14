# Recording what a run changes

How a fleet that already uses Ansible (or Salt, Puppet, kubectl, Terraform, or
plain scripts) can adopt baseline without breaking their existing configs down,
editing them, or hand-listing what to track.

## Problem

Existing recipes use templates, variables, loops, includes, imported roles, and
`command`/`shell` tasks. Hand enumeration of every touched file is impractical.
A recipe parser is fragile: it must model the whole language and still misses
things.

## Principle

**Node inventory**: the set of state a provisioning tool manages on one node,
recorded so baseline can check it. Not Ansible's host inventory (the list of
nodes a play targets) — this is a fileset.

Observe the effect of the run, not the recipe. Snapshot the node before any
state change runs, snapshot it again after, diff to find what changed, and make
that the node inventory. Whatever changed is captured exactly, including files
written by `command`/`shell` and templates resolved at run time. It does not
matter how a file was changed, only whether the run changed it.

## Mechanism

Wrap the change window between `-record-begin` and `-record-end`. With Ansible,
one wrapper play imports the existing configs with `import_playbook`; their
recipes are never edited.

```yaml
- name: Baseline record begin
  hosts: all
  tasks:
    - ansible.builtin.command:
        argv: [baseline-agent, -record-begin, -data, /var/lib/baseline/pre.json,
               -dirs, /etc,/usr/local/etc,/opt,/srv]

- import_playbook: plays/cassandra.yml
- import_playbook: plays/kafka.yml
- import_playbook: plays/nginx.yml

- name: Baseline record end
  hosts: all
  post_tasks:
    - ansible.builtin.command:
        argv: [baseline-agent, -record-end, -manifest, /etc/baseline/manifest.json,
               -data, /var/lib/baseline/pre.json,
               -dirs, /etc,/usr/local/etc,/opt,/srv]
```

The same two commands wrap any other change window, for example a script that
runs `kubectl apply`, a Salt highstate, a Puppet agent run, or a Terraform
`local-exec`:

```sh
baseline-agent -record-begin -data /var/lib/baseline/pre.json -dirs /etc,/opt,/srv
kubectl apply -f deploy/          # or: salt-call state.apply, terraform apply
baseline-agent -record-end -manifest /etc/baseline/manifest.json \
  -data /var/lib/baseline/pre.json -dirs /etc,/opt,/srv
```

All `-record-*` modes run as root: they read every file under the roots
(`/etc` included) and write the index and manifest to root-owned paths. Under
Ansible that means `become: true`.

`record-begin` hashes all regular files under the chosen roots into a pre-index.
`record-end` re-hashes, diffs, adds the changed and new paths to the manifest
file list with their attributes, removes files the run deleted from tracking,
records the installed package and active service snapshots, and writes the
manifest.

On a fresh node one run captures the tool's footprint. On existing nodes the
manifest accumulates the union of deltas across runs, so the tracked surface
grows as the recipes evolve. Re-running an unchanged recipe produces no diff
and does not grow the manifest.

The index file and the manifest are written owner-only. The commands reuse the
agent's hashing and package/service snapshot code.

## Bootstrap an existing node

`-record-begin`/`-record-end` are delta-based, so on an already-provisioned
node a run with no changes yields an empty inventory. To start from today, use
`-record-snapshot`: it tracks every file under the roots as the baseline, no
diff required.

Name the service config directories you actually manage, not all of `/etc`:

```sh
baseline-agent -record-snapshot -manifest /etc/baseline/manifest.json \
  -dirs /etc/nginx,/etc/ssh,/opt/app
```

> **Do not snapshot all of `/etc`.** There is no exclusion list — every regular
> file under a root is tracked. Several files under `/etc` are rewritten by the
> machine itself and become permanent drift that no playbook run can fix:
> `resolv.conf` (DHCP), `ld.so.cache` (any package install), `adjtime` (clock
> sync), `shadow` (any password change). The only way out is re-recording, which
> adopts every *other* deviation at the same time. Keep the roots narrow.

With narrow roots this is the day-one bootstrap: install the agent, snapshot
once, and the node is tracked from then on.

## Controller-side capture (no node binary at record time)

For cookbooks you cannot wrap with the record tasks, Ansible can gather on the
controller instead. Enable the `baseline_capture` callback plugin:

```ini
# ansible.cfg
[defaults]
callbacks_enabled = baseline.fleet.baseline_capture
```

The callback records, per host, every file a file-managing module reported
(`dest`/`path`) and writes `BASELINE_OBSERVED_DIR/<host>.json`. Ship that file to
the node and merge it into the manifest with `-record-observed`:

```sh
baseline-agent -record-observed -manifest /etc/baseline/manifest.json \
  -data /var/lib/baseline/observed/web-01.json
```

The callback only enumerates the paths; `-record-observed` hashes each file on
the node, so the recorded checksum comes from the only source that can vouch
for it. The callback view also misses files written by `command`/`shell`; the
node-side diff catches those. Use the callback as the controller-side option
and the diff as the complete one.

## What is recorded per file

| Attribute | Source | Checkable |
|-----------|--------|-----------|
| sha256 | hash at record time | yes, default |
| mode | stat | yes, default |
| size | stat | optional |
| mtime | stat | optional |

Packages and services need no enumeration: snapshot mode records every
installed package and active service, and the manifest records it.

## Check fidelity

The check compares sha256 and mode by default. Compare size and mtime too when
the operator sets the check set in `agent.toml`, for example
`check = ["sha256", "mode", "mtime"]`. mtime catches files rewritten with
identical content (cron, sed, logrotate-style tools). Size catches truncation.
The attributes are stored at record time, so the fidelity decision can be made
later without re-provisioning.

## Adoption flow

1. Add the wrapper play and the two record commands around the real configs.
2. Run the wrapper once per host type. The manifest now tracks exactly what
   those configs manage.
3. Deploy the agent as a service. It checks the manifest on the audit interval
   and reports to OTel.
4. When a run adds a file, run the wrapper again. `record-end` extends the
   manifest.

## Scope limits

`record-end` captures what the run changed under the chosen roots. Files the
cookbook writes elsewhere are out of scope, and anything not recorded is
invisible. A node state change made after provisioning is drift, which is the
point of checking.