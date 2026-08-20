# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[3]
PRESUBMIT = (ROOT / ".github/workflows/presubmit.yml").read_text(encoding="utf-8")
SECURITY = (ROOT / ".github/workflows/security.yml").read_text(encoding="utf-8")
CACHE_SHA = "55cc8345863c7cc4c66a329aec7e433d2d1c52a9"
REUSABLE_SHA = "22cd42b4f5c08bcb579aed4b6a0bb8cd4696daa0"


for job in ("architecture", "lint", "terraform"):
    pattern = rf"(?m)^  {job}:\n    name: {job}$"
    assert re.search(pattern, PRESUBMIT), f"{job} must have a stable id and display name"

terraform_job = PRESUBMIT.split("\n  terraform:\n", maxsplit=1)[1]
terraform_job = terraform_job.split("\n  # " + "-" * 10, maxsplit=1)[0]
assert "fetch-depth: 0" in terraform_job
assert (
    'TERRAFORM_INTERFACE_BASE_REF: "${{ github.event.pull_request.base.sha || github.event.before }}"'
    in terraform_job
)

assert PRESUBMIT.count(f"actions/cache/restore@{CACHE_SHA}") == 1
assert PRESUBMIT.count(f"actions/cache/save@{CACHE_SHA}") == 1
assert "restore-keys:" not in PRESUBMIT

cache_keys = re.findall(r"(?m)^          key: (tf-google-.+)$", PRESUBMIT)
assert len(cache_keys) == 2 and cache_keys[0] == cache_keys[1]
assert "runner.os" in cache_keys[0] and "runner.arch" in cache_keys[0]
assert "provider-compatibility.toml" in cache_keys[0]
assert ".terraform.lock.hcl" in cache_keys[0]

save_step = PRESUBMIT.split("- name: Save trusted Terraform provider cache", maxsplit=1)[1]
save_step = save_step.split("\n      - name:", maxsplit=1)[0]
assert "github.event_name == 'push'" in save_step
assert "github.ref == 'refs/heads/main'" in save_step
assert "cache-hit != 'true'" in save_step

for workflow in (
    "reusable-go-ci.yml",
    "reusable-rust-ci.yml",
    "reusable-uv-ci.yml",
):
    assert f"{workflow}@{REUSABLE_SHA}" in PRESUBMIT
assert f"reusable-codeql.yml@{REUSABLE_SHA}" in SECURITY
assert "@v1.1.0" not in PRESUBMIT
assert "@v1.1.0" not in SECURITY

print("Terraform workflow trust-boundary assertions passed.")
