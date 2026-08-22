#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

from __future__ import annotations

import json
import os
import platform
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from ci.nix_cache import populate


class PopulationTests(unittest.TestCase):
    def setUp(self) -> None:
        self.contract = populate.load_contract(populate.DEFAULT_CONTRACT)

    def activated_contract(self) -> dict[str, object]:
        contract = json.loads(json.dumps(self.contract))
        contract["activation"] = {"enabled": True, "reason": "qualified"}
        return contract

    def environment(self) -> dict[str, str]:
        return {
            "ATTIC_CACHE_NAME": "mindclade-ci",
            "ATTIC_CACHE_WRITE_TOKEN": "header.payload.signature",
            "ATTIC_SERVER_ENDPOINT": "https://nix-cache.mindclade.com/",
            "GITHUB_EVENT_NAME": "push",
            "GITHUB_REF": "refs/heads/main",
            "GITHUB_REF_PROTECTED": "true",
            "GITHUB_REPOSITORY": "mindclade/mindclade-internal-monorepo",
            "GITHUB_SHA": "a" * 40,
            "GITHUB_WORKFLOW_REF": self.contract["caller_workflow_ref"],
            "MINDCLADE_NIX_CACHE_ACTIVATED": "true",
            "NIX_CACHE_TRUSTED_PUBLIC_KEY": "mindclade-ci-1:YWJjZA==",
            "RUNNER_OS": "Linux",
        }

    def git_result(self, command: list[str]) -> subprocess.CompletedProcess[str]:
        output = "a" * 40 + "\n" if command[1:3] == ["rev-parse", "HEAD"] else ""
        return subprocess.CompletedProcess(command, 0, stdout=output, stderr="")

    def test_checked_in_contract_is_blocked_and_redacted(self) -> None:
        rendered = populate.plan(self.contract)
        self.assertFalse(rendered["activation"]["enabled"])
        self.assertFalse(rendered["client_signing_key_in_scope"])
        self.assertEqual(
            rendered["attic_client_commit"], populate.EXPECTED_ATTIC_CLIENT_COMMIT
        )
        self.assertEqual(
            rendered["dev_shell_installables"],
            [f".#devShells.x86_64-linux.{name}" for name in populate.EXPECTED_SHELLS],
        )
        self.assertNotIn("token", json.dumps(rendered).lower())

    def test_execute_is_blocked_by_checked_in_contract(self) -> None:
        with self.assertRaisesRegex(populate.PopulationError, "population is blocked"):
            populate.authorize(self.contract, self.environment())

    def test_untrusted_event_is_rejected(self) -> None:
        environment = self.environment()
        environment["GITHUB_EVENT_NAME"] = "pull_request"
        with self.assertRaisesRegex(populate.PopulationError, "untrusted GitHub event"):
            populate.authorize(self.activated_contract(), environment)

    def test_unprotected_ref_is_rejected(self) -> None:
        environment = self.environment()
        environment["GITHUB_REF_PROTECTED"] = "false"
        with self.assertRaisesRegex(populate.PopulationError, "must be protected"):
            populate.authorize(self.activated_contract(), environment)

    def test_signing_key_environment_is_rejected(self) -> None:
        environment = self.environment()
        environment["NIX_SECRET_KEY_FILE"] = "/secret/key"
        with self.assertRaisesRegex(populate.PopulationError, "forbidden server/signing"):
            populate.authorize(self.activated_contract(), environment)

    @mock.patch.object(platform, "machine", return_value="x86_64")
    @mock.patch.object(subprocess, "run")
    def test_exact_trusted_main_checkout_is_authorized(
        self, run: mock.Mock, _machine: mock.Mock
    ) -> None:
        run.side_effect = lambda command, **_kwargs: self.git_result(command)
        populate.authorize(self.activated_contract(), self.environment(), repo=Path("/repo"))
        self.assertEqual(run.call_count, 4)
        for call in run.call_args_list:
            self.assertNotIn("ATTIC_CACHE_WRITE_TOKEN", call.kwargs["env"])

    @mock.patch.object(populate, "_run")
    def test_package_inventory_is_sorted_and_exact(self, run: mock.Mock) -> None:
        run.return_value = subprocess.CompletedProcess(
            ["nix"], 0, stdout='["zeta","alpha","alpha"]\n', stderr=""
        )
        self.assertEqual(
            populate.package_installables(Path("/repo")),
            [".#packages.x86_64-linux.alpha", ".#packages.x86_64-linux.zeta"],
        )

    def test_contract_field_drift_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "population.json"
            payload = json.loads(json.dumps(self.contract))
            payload["unexpected"] = True
            path.write_text(json.dumps(payload), encoding="utf-8")
            with self.assertRaisesRegex(populate.PopulationError, "field inventory"):
                populate.load_contract(path)

    @mock.patch.object(populate, "_run")
    def test_publish_uses_token_file_and_verifies_private_key(
        self, run: mock.Mock
    ) -> None:
        environment = self.environment()

        def inspect(command: list[str], **kwargs):
            self.assertNotIn(environment["ATTIC_CACHE_WRITE_TOKEN"], command)
            self.assertNotIn("ATTIC_CACHE_WRITE_TOKEN", kwargs["environment"])
            if command[1:3] == ["cache", "info"]:
                config = Path(kwargs["environment"]["XDG_CONFIG_HOME"]) / "attic/config.toml"
                self.assertEqual(config.stat().st_mode & 0o777, 0o600)
                config_text = config.read_text(encoding="utf-8")
                self.assertIn("token-file = ", config_text)
                self.assertNotIn(environment["ATTIC_CACHE_WRITE_TOKEN"], config_text)
                token_path = Path(
                    next(
                        line.split(" = ", 1)[1]
                        for line in config_text.splitlines()
                        if line.startswith("token-file = ")
                    ).strip('"')
                )
                self.assertEqual(token_path.stat().st_mode & 0o777, 0o600)
                return subprocess.CompletedProcess(
                    command,
                    0,
                    stdout="",
                    stderr=(
                        "Public: false\n"
                        f"Public Key: {environment['NIX_CACHE_TRUSTED_PUBLIC_KEY']}\n"
                    ),
                )
            return subprocess.CompletedProcess(command, 0, stdout="", stderr="")

        run.side_effect = inspect
        populate.publish(
            self.activated_contract(),
            environment,
            ["/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-example"],
            repo=Path("/repo"),
        )
        self.assertEqual(run.call_count, 2)


if __name__ == "__main__":
    unittest.main()
