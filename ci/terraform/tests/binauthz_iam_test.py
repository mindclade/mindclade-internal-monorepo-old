# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Regression assertions for the Binary Authorization issuer trust boundary."""

from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
MODULE = ROOT / "infra/terraform/modules/binauthz"
MAIN = (MODULE / "main.tf").read_text(encoding="utf-8")
OUTPUTS = (MODULE / "outputs.tf").read_text(encoding="utf-8")
README = (MODULE / "README.md").read_text(encoding="utf-8")

FORBIDDEN = "roles/containeranalysis.occurrences.editor"
assert FORBIDDEN not in MAIN, "binauthz module must never grant project-wide occurrence editor"
assert FORBIDDEN not in OUTPUTS, "binauthz outputs must not advertise occurrence editor"

required = {
    "containeranalysis.occurrences.create",
    "containeranalysis.occurrences.get",
    "containeranalysis.occurrences.list",
}
for permission in required:
    assert permission in MAIN, f"binauthz module does not publish required permission: {permission}"
    assert permission in README, f"binauthz caller contract omits required permission: {permission}"

for forbidden_permission in (
    "containeranalysis.occurrences.delete",
    "containeranalysis.occurrences.update",
):
    assert forbidden_permission not in MAIN
    assert forbidden_permission not in OUTPUTS

print("Binary Authorization issuer IAM assertions passed.")
