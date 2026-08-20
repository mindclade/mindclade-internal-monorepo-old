# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Frozen Python projections of the Rust model-worker control messages."""

from __future__ import annotations

import unittest

from mindclade.runtime.v1 import worker_command_pb2, worker_status_pb2


class RuntimeWorkerGoldenTest(unittest.TestCase):
    def test_heartbeat_command_matches_rust_golden(self) -> None:
        command = worker_command_pb2.WorkerCommand(
            sequence=7,
            heartbeat=worker_command_pb2.HeartbeatCommand(requested_at_unix_millis=100),
        )
        self.assertEqual("08072a020864", command.SerializeToString().hex())

    def test_running_status_matches_rust_golden(self) -> None:
        status = worker_status_pb2.WorkerStatus(
            sequence=11,
            ticket_id="ticket",
            fencing_token=9,
            state=worker_status_pb2.WORKER_STATE_RUNNING,
            observed_unix_millis=100,
            message="running",
        )
        self.assertEqual(
            "080b12067469636b6574180920052864320772756e6e696e67",
            status.SerializeToString().hex(),
        )


if __name__ == "__main__":
    unittest.main()
