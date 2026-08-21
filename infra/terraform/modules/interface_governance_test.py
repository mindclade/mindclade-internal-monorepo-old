#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import copy
import importlib
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]
sys.dont_write_bytecode = True
sys.path.insert(0, str(ROOT))

governance = importlib.import_module("infra.terraform.governance.interface_governance")
classify_interfaces = governance.classify_interfaces
verify_scope = governance.verify_scope


def _unit() -> dict:
    return {
        "inputs": {
            "optional": {
                "default": "old",
                "ephemeral": False,
                "nullable": True,
                "required": False,
                "sensitive": False,
                "type": "string",
                "validation_fingerprints": ["sha256:old"],
            },
            "removed": {
                "default": None,
                "ephemeral": False,
                "nullable": True,
                "required": True,
                "sensitive": False,
                "type": "string",
                "validation_fingerprints": [],
            },
        },
        "managed_resource_addresses": ["google_storage_bucket.old"],
        "module_calls": {"module.old": {"source": "../old", "version": None}},
        "moved": [],
        "outputs": {
            "changed": {
                "ephemeral": False,
                "sensitive": False,
                "value_fingerprint": "sha256:old",
            },
            "removed": {
                "ephemeral": False,
                "sensitive": False,
                "value_fingerprint": "sha256:removed",
            },
        },
        "path": "infra/terraform/modules/example",
        "provider_contracts": {
            "google": {
                "configuration_aliases": [],
                "source": "registry.terraform.io/hashicorp/google",
            }
        },
        "requirements": {"google": ">= 7.41.0, < 8.0.0"},
    }


class TerraformModuleGovernanceTest(unittest.TestCase):
    def test_committed_module_scope_matches_hcl(self) -> None:
        self.assertEqual([], verify_scope(ROOT, "modules"))

    def test_breaking_and_compatible_changes_are_classified(self) -> None:
        baseline = {"modules": {"example": _unit()}}
        current = copy.deepcopy(baseline)
        module = current["modules"]["example"]
        del module["inputs"]["removed"]
        module["inputs"]["optional"]["default"] = "new"
        module["inputs"]["optional"]["required"] = True
        module["inputs"]["optional"]["sensitive"] = True
        module["inputs"]["optional"]["type"] = "number"
        module["inputs"]["optional"]["nullable"] = False
        module["inputs"]["optional"]["ephemeral"] = True
        module["inputs"]["optional"]["validation_fingerprints"] = ["sha256:new"]
        module["inputs"]["required_new"] = {
            "default": None,
            "ephemeral": False,
            "nullable": True,
            "required": True,
            "sensitive": False,
            "type": "string",
            "validation_fingerprints": [],
        }
        module["inputs"]["optional_new"] = {
            "default": False,
            "ephemeral": False,
            "nullable": True,
            "required": False,
            "sensitive": False,
            "type": "bool",
            "validation_fingerprints": [],
        }
        module["managed_resource_addresses"] = ["google_storage_bucket.new"]
        module["module_calls"] = {}
        del module["outputs"]["removed"]
        module["outputs"]["changed"]["value_fingerprint"] = "sha256:new"
        module["outputs"]["added"] = {
            "ephemeral": False,
            "sensitive": False,
            "value_fingerprint": "sha256:added",
        }
        module["requirements"]["google"] = ">= 7.45.0, < 8.0.0"
        module["provider_contracts"]["google"] = {
            "configuration_aliases": ["google.secondary"],
            "source": "example.invalid/custom/google",
        }

        changes = {item["id"]: item["breaking"] for item in classify_interfaces(baseline, current)}
        self.assertTrue(changes["module:example:input:removed:removed"])
        self.assertTrue(changes["module:example:input:optional:default-changed"])
        self.assertTrue(changes["module:example:input:optional:requiredness-tightened"])
        self.assertTrue(changes["module:example:input:optional:sensitive-changed"])
        self.assertTrue(changes["module:example:input:optional:type-changed"])
        self.assertTrue(changes["module:example:input:optional:nullable-changed"])
        self.assertTrue(changes["module:example:input:optional:ephemeral-changed"])
        self.assertTrue(changes["module:example:input:optional:validation_fingerprints-changed"])
        self.assertTrue(changes["module:example:input:required_new:added-required"])
        self.assertFalse(changes["module:example:input:optional_new:added-optional"])
        self.assertTrue(changes["module:example:output:removed:removed"])
        self.assertTrue(changes["module:example:output:changed:value_fingerprint-changed"])
        self.assertFalse(changes["module:example:output:added:added"])
        self.assertTrue(changes["module:example:requirement:google:tightened"])
        self.assertTrue(
            changes["module:example:provider-contract:google:configuration_aliases-changed"]
        )
        self.assertTrue(changes["module:example:provider-contract:google:source-changed"])
        self.assertTrue(changes["module:example:resource:google_storage_bucket.old:removed"])
        self.assertFalse(changes["module:example:resource:google_storage_bucket.new:added"])
        self.assertTrue(changes["module:example:module-call:module.old:removed"])

    def test_constraint_tightening_distinguishes_wider_ranges(self) -> None:
        self.assertTrue(
            governance._constraint_tightened(">= 7.41.0, < 8.0.0", ">= 7.45.0, < 8.0.0")
        )
        self.assertTrue(
            governance._constraint_tightened(">= 7.41.0, < 8.0.0", ">= 7.41.0, < 7.50.0")
        )
        self.assertFalse(
            governance._constraint_tightened(">= 7.45.0, < 8.0.0", ">= 7.41.0, < 8.0.0")
        )

    def test_commented_blocks_cannot_spoof_addresses_or_moves(self) -> None:
        with tempfile.TemporaryDirectory() as raw_temp:
            directory = Path(raw_temp)
            (directory / "main.tf").write_text(
                """/*
resource "google_storage_bucket" "fake" {}
module "fake" { source = "../fake" }
moved {
  from = google_storage_bucket.fake
  to   = google_storage_bucket.other_fake
}
*/
# resource "google_storage_bucket" "line_fake" {}
// module "line_fake" { source = "../line-fake" }
resource "google_storage_bucket" "real" {}
module "real" { source = "../real" }
moved {
  from = google_storage_bucket.old
  to   = google_storage_bucket.real
}
""",
                encoding="utf-8",
            )
            self.assertEqual(
                ["google_storage_bucket.real"], governance._managed_addresses(directory)
            )
            self.assertEqual({"module.real"}, governance._declarations(directory, "module"))
            self.assertEqual(
                [
                    {
                        "from": "google_storage_bucket.old",
                        "to": "google_storage_bucket.real",
                    }
                ],
                governance._moved_mappings(directory),
            )

    def test_variable_output_and_provider_behavior_is_fingerprinted(self) -> None:
        with tempfile.TemporaryDirectory() as raw_temp:
            directory = Path(raw_temp)
            (directory / "interface.tf").write_text(
                """terraform {
  required_providers {
    google = {
      source                = "hashicorp/google"
      version               = ">= 7.41.0, < 8.0.0"
      configuration_aliases = [google.secondary, google.primary]
    }
  }
}

variable "name" {
  type      = string
  nullable  = false
  ephemeral = true

  validation {
    condition     = length(var.name) > 2
    error_message = "name is too short"
  }
}

output "shape" {
  value = {
    id   = var.name
    live = true
  }
}
""",
                encoding="utf-8",
            )
            variable = governance._variable_behaviors(directory)["name"]
            output = governance._output_behaviors(directory)["shape"]
            providers = governance._provider_contracts(directory, {"terraform", "google"})
            self.assertFalse(variable["nullable"])
            self.assertTrue(variable["ephemeral"])
            self.assertEqual(1, len(variable["validation_fingerprints"]))
            self.assertTrue(output["value_fingerprint"].startswith("sha256:"))
            self.assertEqual(
                {
                    "google": {
                        "configuration_aliases": ["google.primary", "google.secondary"],
                        "source": "registry.terraform.io/hashicorp/google",
                    }
                },
                providers,
            )

    def test_native_moved_mapping_is_extracted(self) -> None:
        with tempfile.TemporaryDirectory() as raw_temp:
            directory = Path(raw_temp)
            (directory / "moved.tf").write_text(
                "moved {\n  from = google_storage_bucket.old\n"
                "  to   = google_storage_bucket.new\n}\n",
                encoding="utf-8",
            )
            self.assertEqual(
                [
                    {
                        "from": "google_storage_bucket.old",
                        "to": "google_storage_bucket.new",
                    }
                ],
                governance._moved_mappings(directory),
            )

    def test_moved_mapping_removal_and_retargeting_are_breaking(self) -> None:
        baseline = {"modules": {"example": _unit()}}
        baseline["modules"]["example"]["moved"] = [
            {"from": "google_storage_bucket.old", "to": "google_storage_bucket.new"}
        ]
        removed = copy.deepcopy(baseline)
        removed["modules"]["example"]["moved"] = []
        removed_changes = {
            item["id"]: item["breaking"] for item in classify_interfaces(baseline, removed)
        }
        self.assertTrue(removed_changes["module:example:moved:google_storage_bucket.old:removed"])

        retargeted = copy.deepcopy(baseline)
        retargeted["modules"]["example"]["moved"][0]["to"] = "google_storage_bucket.different"
        retargeted_changes = {
            item["id"]: item["breaking"] for item in classify_interfaces(baseline, retargeted)
        }
        self.assertTrue(
            retargeted_changes["module:example:moved:google_storage_bucket.old:target-changed"]
        )

    def test_breaking_migration_record_is_required_and_validated(self) -> None:
        baseline = {
            "contract_version": "0.1.0",
            "modules": {"example": _unit()},
        }
        current = copy.deepcopy(baseline)
        current["contract_version"] = "0.2.0"
        module = current["modules"]["example"]
        module["managed_resource_addresses"] = ["google_storage_bucket.new"]
        changes = classify_interfaces(baseline, current)
        breaking_ids = sorted(item["id"] for item in changes if item["breaking"])

        with tempfile.TemporaryDirectory() as raw_temp:
            repo = Path(raw_temp)
            migrations = repo / "infra/terraform/governance/migrations"
            migrations.mkdir(parents=True)
            evidence = repo / "evidence.md"
            evidence.write_text("qualified fixture\n", encoding="utf-8")
            missing = governance._validate_migrations(repo, baseline, current, changes)
            self.assertTrue(any("no migration record" in error for error in missing))
            identifiers = ",\n".join(f'  "{identifier}"' for identifier in breaking_ids)
            record = migrations / "0.1.0-to-0.2.0.toml"
            common = f"""schema_version = {governance.MIGRATION_SCHEMA_VERSION}
from_version = "0.1.0"
to_version = "0.2.0"
status = "planned"
owner = "platform-control"
summary = "fixture migration"
affected_modules = ["example"]
breaking_change_ids = [
{identifiers},
]
consumer_steps = ["review the saved plan"]
rollback_steps = ["restore the prior source pin"]
qualification_evidence = ["evidence.md"]
"""
            record.write_text(common, encoding="utf-8")
            no_disposition = governance._validate_migrations(repo, baseline, current, changes)
            self.assertTrue(
                any("lack state_move or intentional_removal" in error for error in no_disposition)
            )

            record.write_text(
                common
                + """
[[state_move]]
module = "example"
from = "google_storage_bucket.old"
to = "google_storage_bucket.new"
""",
                encoding="utf-8",
            )
            missing_native = governance._validate_migrations(repo, baseline, current, changes)
            self.assertTrue(any("lacks a native moved block" in error for error in missing_native))
            module["moved"] = [
                {"from": "google_storage_bucket.old", "to": "google_storage_bucket.new"}
            ]
            changes = classify_interfaces(baseline, current)
            self.assertEqual([], governance._validate_migrations(repo, baseline, current, changes))

    def test_intentional_removal_is_an_explicit_valid_disposition(self) -> None:
        baseline = {"contract_version": "0.1.0", "modules": {"example": _unit()}}
        current = copy.deepcopy(baseline)
        current["contract_version"] = "0.2.0"
        current["modules"]["example"]["managed_resource_addresses"] = []
        changes = classify_interfaces(baseline, current)
        breaking_ids = sorted(item["id"] for item in changes if item["breaking"])
        with tempfile.TemporaryDirectory() as raw_temp:
            repo = Path(raw_temp)
            migrations = repo / "infra/terraform/governance/migrations"
            migrations.mkdir(parents=True)
            (repo / "evidence.md").write_text("fixture\n", encoding="utf-8")
            identifiers = ",\n".join(f'  "{identifier}"' for identifier in breaking_ids)
            (migrations / "record.toml").write_text(
                f"""schema_version = {governance.MIGRATION_SCHEMA_VERSION}
from_version = "0.1.0"
to_version = "0.2.0"
status = "planned"
owner = "platform-control"
summary = "intentional removal fixture"
affected_modules = ["example"]
breaking_change_ids = [
{identifiers},
]
consumer_steps = ["review"]
rollback_steps = ["restore"]
qualification_evidence = ["evidence.md"]

[[intentional_removal]]
module = "example"
address = "google_storage_bucket.old"
reason = "the resource authority is intentionally retired"
consumer_action = "confirm the saved plan deletes only the old address"
rollback_action = "restore the prior module source and inputs"
""",
                encoding="utf-8",
            )
            self.assertEqual([], governance._validate_migrations(repo, baseline, current, changes))

    def test_base_ref_must_resolve_before_verified_fallback(self) -> None:
        with tempfile.TemporaryDirectory() as raw_temp:
            repo = Path(raw_temp)
            baseline_path = repo / governance.BASELINE_RELATIVE
            baseline_path.parent.mkdir(parents=True)
            commit = "a" * 40
            expected = {
                "contract_version": "0.1.1",
                "modules": {},
                "schema_version": governance.MANIFEST_SCHEMA_VERSION,
                "source_revision": commit,
                "status": "released",
            }
            baseline_path.write_text(governance._json_text(expected), encoding="utf-8")
            policy = {
                "baseline_version": "0.1.1",
                "baseline_commit": commit,
            }
            invalid = SimpleNamespace(returncode=1, stdout="", stderr="missing")
            with (
                patch.object(governance, "_run", return_value=invalid),
                self.assertRaisesRegex(governance.GovernanceError, "invalid or unfetched"),
            ):
                governance._base_manifest(repo, "missing-base-ref", policy, "terraform-docs")

            responses = iter(
                (
                    SimpleNamespace(returncode=0, stdout=f"{commit}\n", stderr=""),
                    SimpleNamespace(returncode=0, stdout="", stderr=""),
                    SimpleNamespace(returncode=0, stdout=f"{commit}\n", stderr=""),
                )
            )
            with (
                patch.object(
                    governance,
                    "_run",
                    side_effect=lambda *_args, **_kwargs: next(responses),
                ),
                patch.object(governance, "_rebuild_baseline_manifest", return_value=expected),
            ):
                self.assertEqual(
                    expected,
                    governance._base_manifest(repo, commit, policy, "terraform-docs"),
                )

            mismatched = copy.deepcopy(expected)
            mismatched["source_revision"] = "b" * 40
            baseline_path.write_text(governance._json_text(mismatched), encoding="utf-8")
            responses = iter(
                (
                    SimpleNamespace(returncode=0, stdout=f"{commit}\n", stderr=""),
                    SimpleNamespace(returncode=0, stdout="", stderr=""),
                )
            )
            with (
                patch.object(
                    governance,
                    "_run",
                    side_effect=lambda *_args, **_kwargs: next(responses),
                ),
                self.assertRaisesRegex(governance.GovernanceError, "revision differs"),
            ):
                governance._base_manifest(repo, commit, policy, "terraform-docs")

            tampered = copy.deepcopy(expected)
            tampered["modules"] = {"forged": _unit()}
            baseline_path.write_text(governance._json_text(tampered), encoding="utf-8")
            responses = iter(
                (
                    SimpleNamespace(returncode=0, stdout=f"{commit}\n", stderr=""),
                    SimpleNamespace(returncode=0, stdout="", stderr=""),
                    SimpleNamespace(returncode=0, stdout=f"{commit}\n", stderr=""),
                )
            )
            with (
                patch.object(
                    governance,
                    "_run",
                    side_effect=lambda *_args, **_kwargs: next(responses),
                ),
                patch.object(governance, "_rebuild_baseline_manifest", return_value=expected),
                self.assertRaisesRegex(governance.GovernanceError, "content does not match"),
            ):
                governance._base_manifest(repo, commit, policy, "terraform-docs")

    def test_same_planned_candidate_uses_immutable_cumulative_baseline(self) -> None:
        released = {
            "contract_version": "0.1.1",
            "modules": {"example": _unit()},
            "schema_version": governance.MANIFEST_SCHEMA_VERSION,
            "source_revision": "a" * 40,
            "status": "released",
        }
        base = copy.deepcopy(released)
        base.update(
            {
                "contract_version": "0.4.0",
                "source_revision": "working-tree",
                "status": "planned",
            }
        )
        current = copy.deepcopy(base)
        current["modules"]["example"]["inputs"]["optional"][
            "validation_fingerprints"
        ] = ["sha256:new"]
        policy = {"contract_version": "0.4.0", "status": "planned"}

        with patch.object(
            governance, "_verified_fallback_manifest", return_value=released
        ) as fallback:
            selected = governance._compatibility_baseline(
                Path("/unused"), base, current, policy, "terraform-docs"
            )

        fallback.assert_called_once()
        self.assertIs(selected, released)
        changes = classify_interfaces(selected, current)
        self.assertIn(
            "module:example:input:optional:validation_fingerprints-changed",
            {change["id"] for change in changes if change["breaking"]},
        )
        self.assertEqual([], governance._validate_version_change(selected, current, changes))

    def test_same_planned_candidate_rejects_uncovered_cumulative_breaking_change(self) -> None:
        released = {
            "contract_version": "0.1.1",
            "modules": {"example": _unit()},
            "schema_version": governance.MANIFEST_SCHEMA_VERSION,
            "source_revision": "a" * 40,
            "status": "released",
        }
        base = copy.deepcopy(released)
        base.update(
            {
                "contract_version": "0.4.0",
                "source_revision": "working-tree",
                "status": "planned",
            }
        )
        base["modules"]["example"]["inputs"]["optional"]["default"] = "candidate"
        current = copy.deepcopy(base)
        current["modules"]["example"]["inputs"]["optional"][
            "validation_fingerprints"
        ] = ["sha256:new"]
        policy = {"contract_version": "0.4.0", "status": "planned"}

        with (
            tempfile.TemporaryDirectory() as raw_temp,
            patch.object(governance, "_verified_fallback_manifest", return_value=released),
        ):
            repo = Path(raw_temp)
            migrations = repo / "infra/terraform/governance/migrations"
            migrations.mkdir(parents=True)
            (repo / "evidence.md").write_text("fixture\n", encoding="utf-8")
            (migrations / "0.1.1-to-0.4.0.toml").write_text(
                f"""schema_version = {governance.MIGRATION_SCHEMA_VERSION}
from_version = "0.1.1"
to_version = "0.4.0"
status = "planned"
owner = "platform-control"
summary = "incomplete cumulative fixture"
affected_modules = ["example"]
breaking_change_ids = [
  "module:example:input:optional:default-changed",
]
consumer_steps = ["review the complete candidate"]
rollback_steps = ["retain the released source pin"]
qualification_evidence = ["evidence.md"]
""",
                encoding="utf-8",
            )
            selected = governance._compatibility_baseline(
                repo, base, current, policy, "terraform-docs"
            )
            changes = classify_interfaces(selected, current)
            errors = governance._validate_migrations(repo, selected, current, changes)

        self.assertTrue(
            any(
                "module:example:input:optional:validation_fingerprints-changed" in error
                for error in errors
            ),
            errors,
        )

    def test_released_same_version_mutation_cannot_use_planned_fallback(self) -> None:
        base = {
            "contract_version": "0.4.0",
            "modules": {"example": _unit()},
            "schema_version": governance.MANIFEST_SCHEMA_VERSION,
            "source_revision": "a" * 40,
            "status": "released",
        }
        current = copy.deepcopy(base)
        current["modules"]["example"]["inputs"]["optional"]["default"] = "new"
        policy = {"contract_version": "0.4.0", "status": "released"}

        with patch.object(governance, "_verified_fallback_manifest") as fallback:
            selected = governance._compatibility_baseline(
                Path("/unused"), base, current, policy, "terraform-docs"
            )

        fallback.assert_not_called()
        changes = classify_interfaces(selected, current)
        self.assertIn(
            "interface changes require a version increment: 0.4.0 -> 0.4.0",
            governance._validate_version_change(selected, current, changes),
        )

    def test_planned_to_released_transition_cannot_bundle_interface_change(self) -> None:
        base = {
            "contract_version": "0.4.0",
            "modules": {"example": _unit()},
            "schema_version": governance.MANIFEST_SCHEMA_VERSION,
            "source_revision": "working-tree",
            "status": "planned",
        }
        current = copy.deepcopy(base)
        current["status"] = "released"
        current["modules"]["example"]["inputs"]["optional"]["default"] = "new"
        policy = {"contract_version": "0.4.0", "status": "released"}

        with patch.object(governance, "_verified_fallback_manifest") as fallback:
            selected = governance._compatibility_baseline(
                Path("/unused"), base, current, policy, "terraform-docs"
            )

        fallback.assert_not_called()
        changes = classify_interfaces(selected, current)
        self.assertIn(
            "interface changes require a version increment: 0.4.0 -> 0.4.0",
            governance._validate_version_change(selected, current, changes),
        )

    def test_planned_candidate_requires_policy_version_and_status_parity(self) -> None:
        base = {
            "contract_version": "0.4.0",
            "modules": {},
            "schema_version": governance.MANIFEST_SCHEMA_VERSION,
            "source_revision": "working-tree",
            "status": "planned",
        }
        current = copy.deepcopy(base)
        for policy in (
            {"contract_version": "0.5.0", "status": "planned"},
            {"contract_version": "0.4.0", "status": "released"},
        ):
            with self.subTest(policy=policy), self.assertRaisesRegex(
                governance.GovernanceError, "differs from version.toml"
            ):
                governance._compatibility_baseline(
                    Path("/unused"), base, current, policy, "terraform-docs"
                )

    def test_manifest_status_is_fail_closed(self) -> None:
        manifest = {
            "contract_version": "0.4.0",
            "modules": {},
            "schema_version": governance.MANIFEST_SCHEMA_VERSION,
            "source_revision": "working-tree",
            "status": "mutable",
        }
        with self.assertRaisesRegex(governance.GovernanceError, "status must be"):
            governance._verify_manifest_shape(manifest, "fixture")


if __name__ == "__main__":
    unittest.main()
