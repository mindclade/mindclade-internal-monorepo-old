# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[3]
PRESUBMIT = (ROOT / ".github/workflows/presubmit.yml").read_text(encoding="utf-8")
NIGHTLY = (ROOT / ".github/workflows/nightly.yml").read_text(encoding="utf-8")
SECURITY = (ROOT / ".github/workflows/security.yml").read_text(encoding="utf-8")
CACHE_SHA = "55cc8345863c7cc4c66a329aec7e433d2d1c52a9"
REUSABLE_SHA = "7e4b7a873fc9312c2985ed262b251455c71756fe"


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

assert terraform_job.count(f"actions/cache/restore@{CACHE_SHA}") == 1
assert terraform_job.count(f"actions/cache/save@{CACHE_SHA}") == 1
assert "restore-keys:" not in terraform_job

cache_keys = re.findall(r"(?m)^          key: (tf-google-.+)$", terraform_job)
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
assert "mindclade-org/.github" not in PRESUBMIT
assert "mindclade-org/.github" not in SECURITY
assert "build-mode: autobuild" in SECURITY
assert "@v1.1.0" not in PRESUBMIT
assert "@v1.1.0" not in SECURITY

python_audit_job = SECURITY.split("\n  python-dependency-audit:\n", maxsplit=1)[1]
python_audit_job = python_audit_job.split("\n  go-vulnerability:\n", maxsplit=1)[0]
assert python_audit_job.count("pip-audit==2.10.1") == 2
assert python_audit_job.count("--require-hashes") == 2
assert python_audit_job.count("--requirement requirements.lock.txt") == 2
assert python_audit_job.count("--vulnerability-service osv") == 1
assert "torch's Linux +cpu wheel" in python_audit_job

bazel_job = PRESUBMIT.split("\n  bazel:\n", maxsplit=1)[1]
bazel_job = bazel_job.split("\n  # Stable, always-reported", maxsplit=1)[0]
assert "timeout-minutes: 90" in bazel_job
affected_executor = (ROOT / "ci/common/affected.py").read_text(encoding="utf-8")
assert bazel_job.count(f"actions/cache/restore@{CACHE_SHA}") == 1
assert bazel_job.count(f"actions/cache/save@{CACHE_SHA}") == 1
for start, end in (
    ("- name: Restore trusted Bazel", "- name: Configure bounded Bazel"),
    ("- name: Measure bounded Bazel", "- name: Save trusted Bazel"),
    ("- name: Save trusted Bazel", "- name: Record Bazel"),
):
    assert (
        "continue-on-error: true" in bazel_job.split(start, maxsplit=1)[1].split(end, maxsplit=1)[0]
    )
assert "--build_event_json_file=" in affected_executor
assert "bazel-performance-${{ github.run_id }}-${{ github.run_attempt }}" in bazel_job
assert "tools/dev/bazelw query '//...' --config=ci" in bazel_job
assert re.search(r"smoke_test --config=ci\s*$", bazel_job, re.MULTILINE)

bazel_cache_keys = re.findall(
    r"(?m)^          key: (\$\{\{ steps\.bazel-cache-trust.+)$", bazel_job
)
assert len(bazel_cache_keys) == 2 and bazel_cache_keys[0] == bazel_cache_keys[1]
assert "bazel-cache-trust.outputs.primary-key" in bazel_cache_keys[0]
assert "restore-keys:" in bazel_job
for locked_input in (
    ".bazelversion",
    "MODULE.bazel",
    "MODULE.bazel.lock",
    "REPO.bazel",
    "flake.lock",
    "tools/build/nix/toolchain-manifest.json",
):
    assert locked_input in bazel_job
for trusted_base in (
    "github.event.pull_request.base.sha",
    "github.event.pull_request.base.ref",
    "github.event.merge_group.base_sha",
    "github.event.merge_group.base_ref",
):
    assert trusted_base in bazel_job
assert "github.event_name == 'push'" in bazel_job
assert "github.ref == 'refs/heads/main'" in bazel_job
assert "github.ref_protected == true" in bazel_job
assert '--ref-protected "${REF_PROTECTED}"' in bazel_job
for command in ("select-trust", "configure", "measure", "record-metrics"):
    assert f"ci/common/bazel_disk_cache.py {command}" in bazel_job
assert "steps.bazel-cache-size.outputs.within-limit == 'true'" in bazel_job
assert "steps.bazel-disk-cache.outcome" in bazel_job
assert "steps.bazel-cache-size.outcome" in bazel_job
assert "--bazel-wrapper" in bazel_job
assert "--remote_cache=" not in bazel_job
assert "user.bazelrc" in (ROOT / ".gitignore").read_text(encoding="utf-8")

nightly_job = NIGHTLY.split("\n  bazel-nightly:\n", maxsplit=1)[1]
assert "if: github.ref == 'refs/heads/main'" in nightly_job
assert nightly_job.count(f"actions/cache/restore@{CACHE_SHA}") == 1
assert nightly_job.count(f"actions/cache/save@{CACHE_SHA}") == 1
for start, end in (
    ("- name: Restore trusted nightly Bazel", "- name: Configure bounded nightly Bazel"),
    ("- name: Measure bounded nightly Bazel", "- name: Save trusted nightly Bazel"),
    ("- name: Save trusted nightly Bazel", "- name: Record nightly Bazel"),
):
    assert (
        "continue-on-error: true"
        in nightly_job.split(start, maxsplit=1)[1].split(end, maxsplit=1)[0]
    )
nightly_cache_keys = re.findall(
    r"(?m)^          key: (\$\{\{ steps\.bazel-cache-trust.+)$", nightly_job
)
assert len(nightly_cache_keys) == 2 and nightly_cache_keys[0] == nightly_cache_keys[1]
assert "restore-keys:" in nightly_job
assert "github.event_name == 'schedule'" in nightly_job
assert "github.event_name == 'workflow_dispatch'" in nightly_job
assert "github.ref_protected == true" in nightly_job
assert '--ref-protected "${REF_PROTECTED}"' in nightly_job
assert "steps.bazel-cache-trust.outputs.role == 'writer'" in nightly_job
assert "steps.bazel-cache-size.outputs.within-limit == 'true'" in nightly_job
assert "steps.bazel-disk-cache.outcome" in nightly_job
assert "steps.bazel-cache-size.outcome" in nightly_job
assert "--bazel-wrapper" in nightly_job
for command in ("select-trust", "configure", "measure", "record-metrics"):
    assert f"ci/common/bazel_disk_cache.py {command}" in nightly_job
assert "ci/nightly/pipeline.py" in nightly_job
assert "--mode affected" not in nightly_job

print("Terraform and Bazel workflow trust-boundary assertions passed.")
