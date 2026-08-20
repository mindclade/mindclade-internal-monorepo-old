# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import copy
import importlib.util
import json
import shutil
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
VALIDATOR_PATH = REPO_ROOT / ".buildkite/scripts/validate_wif_contract.py"
SPEC = importlib.util.spec_from_file_location("validate_wif_contract", VALIDATOR_PATH)
if SPEC is None or SPEC.loader is None:  # pragma: no cover - importlib platform failure.
    raise RuntimeError(f"cannot load {VALIDATOR_PATH}")
VALIDATOR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VALIDATOR)


class BuildkiteWIFContractTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        shutil.copytree(REPO_ROOT / ".buildkite", self.root / ".buildkite")
        self.contract_path = self.root / ".buildkite/contracts/wif-preflight.json"

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def contract(self) -> dict[str, object]:
        return json.loads(self.contract_path.read_text(encoding="utf-8"))

    def write_contract(self, value: dict[str, object]) -> None:
        self.contract_path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")

    def active_contract(self) -> dict[str, object]:
        value = copy.deepcopy(self.contract())
        value["activation_state"] = "active"
        value["organization_id"] = "11111111-1111-4111-8111-111111111111"
        value["wif_provider_audience"] = (
            "https://iam.googleapis.com/projects/236815131283/locations/global/"
            "workloadIdentityPools/buildkite/providers/buildkite"
        )
        value["queue"]["cluster_id"] = "88888888-8888-4888-8888-888888888888"
        value["queue"]["id"] = "99999999-9999-4999-8999-999999999999"
        pipelines = value["pipelines"]
        pipelines["build"]["id"] = "22222222-2222-4222-8222-222222222222"
        pipelines["build"]["service_account"] = "sa-artifact-builder@mc-common-ci.iam.gserviceaccount.com"
        pipelines["qualification"]["id"] = "33333333-3333-4333-8333-333333333333"
        pipelines["qualification"]["service_account"] = (
            "sa-artifact-qualifier@mc-common-ci.iam.gserviceaccount.com"
        )
        pipelines["promotion"]["id"] = "44444444-4444-4444-8444-444444444444"
        pipelines["promotion"]["service_account"] = "sa-artifact-promoter@mc-common-ci.iam.gserviceaccount.com"
        return value

    def runtime_environment(self, contract: dict[str, object], stage: str, expectation: str) -> dict[str, str]:
        pipeline = contract["pipelines"][stage]
        step_key = pipeline["step_key"] if expectation == "allowed" else pipeline["denied_step_key"]
        return {
            "BUILDKITE_AGENT_ID": "55555555-5555-4555-8555-555555555555",
            "BUILDKITE_AGENT_META_DATA_QUEUE": "mindclade-artifact-private",
            "BUILDKITE_BRANCH": "main",
            "BUILDKITE_CLUSTER_ID": contract["queue"]["cluster_id"],
            "BUILDKITE_COMPUTE_TYPE": "self-hosted",
            "BUILDKITE_COMMIT": "a" * 40,
            "BUILDKITE_COMMIT_RESOLVED": "true",
            "BUILDKITE_GIT_COMMIT_VERIFICATION": "strict",
            "BUILDKITE_JOB_ID": "66666666-6666-4666-8666-666666666666",
            "BUILDKITE_ORGANIZATION_ID": contract["organization_id"],
            "BUILDKITE_ORGANIZATION_SLUG": "mindclade",
            "BUILDKITE_PIPELINE_DEFAULT_BRANCH": "main",
            "BUILDKITE_PIPELINE_ID": pipeline["id"],
            "BUILDKITE_PIPELINE_PROVIDER": "github",
            "BUILDKITE_PIPELINE_SLUG": pipeline["slug"],
            "BUILDKITE_PULL_REQUEST": "false",
            "BUILDKITE_REPO": "git@github.com:mindclade/mindclade-internal-monorepo.git",
            "BUILDKITE_SOURCE": "webhook",
            "BUILDKITE_STEP_ID": "77777777-7777-4777-8777-777777777777",
            "BUILDKITE_STEP_KEY": step_key,
        }

    def test_unprovisioned_source_contract_is_valid_and_fail_closed(self) -> None:
        value = VALIDATOR.validate_source(self.contract_path, self.root)
        self.assertEqual(value["activation_state"], "unprovisioned")
        with self.assertRaisesRegex(VALIDATOR.ContractError, "unprovisioned"):
            VALIDATOR.validate_runtime(value, "build", "allowed", {}, "a" * 40)

    def test_partial_activation_is_rejected(self) -> None:
        value = self.contract()
        value["organization_id"] = "11111111-1111-4111-8111-111111111111"
        with self.assertRaisesRegex(VALIDATOR.ContractError, "partial live identity"):
            VALIDATOR.validate_contract(value)

    def test_active_contract_requires_distinct_pipeline_ids(self) -> None:
        value = self.active_contract()
        value["pipelines"]["qualification"]["id"] = value["pipelines"]["build"]["id"]
        with self.assertRaisesRegex(VALIDATOR.ContractError, "distinct pipeline UUIDs"):
            VALIDATOR.validate_contract(value)

    def test_pipeline_step_key_drift_is_rejected(self) -> None:
        path = self.root / ".buildkite/pipelines/artifact-build.yml"
        text = path.read_text(encoding="utf-8")
        path.write_text(text.replace("key: artifact-build\n", "key: artifact-build-mutated\n", 1), encoding="utf-8")
        with self.assertRaisesRegex(VALIDATOR.ContractError, "allowed step drifted"):
            VALIDATOR.validate_source(self.contract_path, self.root)

    def test_privileged_operation_in_identity_canary_is_rejected(self) -> None:
        path = self.root / ".buildkite/scripts/wif-preflight.sh"
        path.write_text(path.read_text(encoding="utf-8") + "\nkubectl apply -f forbidden.yml\n", encoding="utf-8")
        with self.assertRaisesRegex(VALIDATOR.ContractError, "forbidden privileged operation"):
            VALIDATOR.validate_source(self.contract_path, self.root)

    def test_negative_canary_requires_exact_attribute_condition_rejection(self) -> None:
        path = self.root / ".buildkite/scripts/wif-preflight.sh"
        text = path.read_text(encoding="utf-8")
        path.write_text(text.replace('grep -Fqi "invalid_grant"', "grep -Fqi rejected", 1), encoding="utf-8")
        with self.assertRaisesRegex(VALIDATOR.ContractError, "missing required token-exchange fragment"):
            VALIDATOR.validate_source(self.contract_path, self.root)

    def test_allowed_runtime_requires_exact_immutable_context(self) -> None:
        value = self.active_contract()
        VALIDATOR.validate_contract(value)
        environ = self.runtime_environment(value, "qualification", "allowed")
        audience, provider, service_account = VALIDATOR.validate_runtime(
            value, "qualification", "allowed", environ, "a" * 40
        )
        self.assertTrue(audience.startswith("https://iam.googleapis.com/"))
        self.assertTrue(provider.startswith("projects/236815131283/"))
        self.assertTrue(service_account.startswith("sa-artifact-qualifier@"))

    def test_wrong_queue_and_wrong_step_are_rejected_before_oidc(self) -> None:
        value = self.active_contract()
        environ = self.runtime_environment(value, "promotion", "allowed")
        environ["BUILDKITE_AGENT_META_DATA_QUEUE"] = "default"
        environ["BUILDKITE_STEP_KEY"] = "artifact-promote-wif-denied"
        with self.assertRaises(VALIDATOR.ContractError):
            VALIDATOR.validate_runtime(value, "promotion", "allowed", environ, "a" * 40)


if __name__ == "__main__":
    unittest.main()
