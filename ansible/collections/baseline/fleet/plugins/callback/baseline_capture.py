#!/usr/bin/python
# -*- coding: utf-8 -*-
"""Controller-side capture for baseline.

Runs inside ansible-playbook on the controller. For every file-managing task
it records the final path plus the checksum/mode the module reported, and at
the end of the play writes one observed file per host to BASELINE_OBSERVED_DIR
(default /var/lib/baseline/observed/<host>.json).

The callback only captures what the controller already knows from module
results; it does not re-implement any drift logic. Push each host's file to
the node and run `baseline-agent -record-observed` there to build the manifest.

Enable in ansible.cfg:
    [defaults]
    callbacks_enabled = baseline.fleet.baseline_capture
    ANSIBLE_CALLBACK_PLUGINS  # only needed when the collection is not on the
                              # default collection search path

Known limits: it sees module results, so files written by command/shell tasks
are not captured. The node-side -record-begin/-record-end diff catches those.
"""

from ansible.plugins.callback import CallbackBase

import json
import os
import re

DOCUMENTATION = r"""
---
module: baseline_capture
short_description: Capture files changed by an Ansible run on the controller
version_added: "0.3.0"
description:
  - Records per-host file paths (and their checksums/modes) reported by
    file-managing modules during a play.
  - Writes one JSON file per host to BASELINE_OBSERVED_DIR.
"""

FILE_MODULES = {
    "copy",
    "template",
    "lineinfile",
    "blockinfile",
    "file",
    "assemble",
    "get_url",
}


class CallbackModule(CallbackBase):
    """Capture file-managing module results and write per-host observed files."""

    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "notification"
    CALLBACK_NAME = "baseline_capture"
    CALLBACK_NEEDS_WHITELIST = True

    def __init__(self):
        super(CallbackModule, self).__init__()
        self.out_dir = os.environ.get("BASELINE_OBSERVED_DIR", "/var/lib/baseline/observed")
        self.observed = {}

    def v2_playbook_on_play_start(self, play):
        if self.observed == {}:
            self.observed = {}

    def v2_runner_on_ok(self, result, **kwargs):
        action = getattr(getattr(result, "_task", None), "action", "") or ""
        action = action.split(".")[-1]
        if action not in FILE_MODULES:
            return

        res = result._result or {}
        path = res.get("dest") or res.get("path")
        if not path or not isinstance(path, str):
            return

        checksum = res.get("checksum") or (res.get("stat") or {}).get("checksum")
        mode = res.get("mode")
        if isinstance(mode, int):
            mode = oct(mode & 0o777)[2:]
        entry = {}
        if checksum:
            entry["sha256"] = checksum
        if mode:
            entry["mode"] = str(mode)
        self.observed.setdefault(result._host.get_name(), {})[path] = entry

    def v2_playbook_on_stats(self, stats):
        try:
            os.makedirs(self.out_dir, exist_ok=True)
        except OSError:
            return
        for host, files in self.observed.items():
            safe_host = re.sub(r"[^\w.-]", "_", host)
            with open(os.path.join(self.out_dir, safe_host + ".json"), "w") as fh:
                json.dump(files, fh, indent=2, sort_keys=True)