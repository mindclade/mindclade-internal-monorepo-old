# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Frozen protobuf bytes shared by training, artifact, and registry contracts."""

from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path
from typing import Any

from mindclade.artifact.v1 import checkpoint_pb2 as artifact_checkpoint_pb2
from mindclade.registry.v1 import checkpoint_pb2 as registry_checkpoint_pb2
from mindclade.training.v1 import checkpoint_pb2, topology_pb2

FIXTURE = (
    Path(__file__).resolve().parents[5]
    / "tests/integration/cross_language/fixtures/checkpoint_contract_v1.json"
)


def _artifact(reference: Any, seed: str, logical_kind: str, size_bytes: int) -> None:
    reference.digest = f"sha256:{seed * 64}"
    reference.size_bytes = size_bytes
    reference.media_type = (
        "application/vnd.mindclade." + logical_kind.replace(".", "-") + ".v1+proto"
    )
    reference.logical_kind = logical_kind
    reference.schema_version = 1


def _topology() -> topology_pb2.TrainingTopology:
    return topology_pb2.TrainingTopology(
        world_size=2,
        local_world_size=2,
        node_count=1,
        data_parallel_size=2,
        tensor_parallel_size=1,
        pipeline_parallel_size=1,
        backend=topology_pb2.COLLECTIVE_BACKEND_GLOO,
        device_type=topology_pb2.TRAINING_DEVICE_TYPE_CPU,
        topology_fingerprint=f"sha256:{'a' * 64}",
    )


def _manifest() -> checkpoint_pb2.CheckpointManifest:
    manifest = checkpoint_pb2.CheckpointManifest(
        schema_version=1,
        checkpoint_id="checkpoint_019c0000000070008000000000000001",
        run_id="run_019c0000000070008000000000000002",
        attempt_id="attempt-2",
        checkpoint_attempt=2,
        data_position=32,
    )
    manifest.counters.microbatches = 8
    manifest.counters.optimizer_steps = 4
    manifest.counters.samples = 32
    for reference, seed, kind, size in (
        (manifest.resolved_config, "1", "training.resolved-config", 128),
        (manifest.dataset, "2", "training.dataset", 256),
        (manifest.model, "3", "training.model", 64),
        (manifest.source, "4", "source.archive", 512),
        (manifest.toolchain, "5", "toolchain.manifest", 128),
        (manifest.environment, "6", "training.environment", 128),
        (
            manifest.compatibility_policy,
            "8",
            "training.checkpoint.compatibility-policy",
            96,
        ),
    ):
        _artifact(reference, seed, kind, size)
    manifest.topology.CopyFrom(_topology())
    component = manifest.components.add(
        name="model",
        kind=checkpoint_pb2.CHECKPOINT_COMPONENT_KIND_MODEL,
        relative_path="rank-00000/model.safetensors",
        rank=0,
        tensor_layout=checkpoint_pb2.CHECKPOINT_TENSOR_LAYOUT_REPLICATED,
        tensor_fqns=["bias", "scale"],
    )
    _artifact(component.artifact, "7", "training.checkpoint.tensors", 1024)
    manifest.created_at.seconds = 1800000000
    return manifest


def _commit() -> artifact_checkpoint_pb2.CheckpointCommit:
    manifest = _manifest()
    commit = artifact_checkpoint_pb2.CheckpointCommit(
        schema_version=1,
        checkpoint_id=manifest.checkpoint_id,
        run_id=manifest.run_id,
        checkpoint_attempt=2,
        fencing_token=9,
        writer_id="checkpoint-agent-0",
        commit_digest=f"sha256:{'b' * 64}",
    )
    _artifact(commit.manifest, "9", "training.checkpoint.manifest", 2048)
    commit.committed_at.seconds = 1800000000
    return commit


def _record() -> registry_checkpoint_pb2.CheckpointRecord:
    commit = _commit()
    record = registry_checkpoint_pb2.CheckpointRecord(
        schema_version=1,
        checkpoint_id=commit.checkpoint_id,
        run_id=commit.run_id,
        optimizer_step=4,
        checkpoint_attempt=2,
        fencing_token=9,
        topology_fingerprint=f"sha256:{'a' * 64}",
        lifecycle=registry_checkpoint_pb2.CHECKPOINT_LIFECYCLE_COMMITTED,
        policy_epoch=3,
        resource_version=1,
        record_digest=f"sha256:{'c' * 64}",
    )
    record.manifest.CopyFrom(commit.manifest)
    _artifact(record.commit, "a", "artifact.checkpoint.commit", 512)
    record.created_at.seconds = 1800000000
    record.retain_until.seconds = 1800086400
    return record


def _fixture_payload() -> dict[str, int | str]:
    messages = {
        "topology_hex": _topology(),
        "manifest_hex": _manifest(),
        "commit_hex": _commit(),
        "record_hex": _record(),
    }
    return {
        "schema_version": 1,
        **{
            field: message.SerializeToString(deterministic=True).hex()
            for field, message in messages.items()
        },
    }


class CheckpointContractGoldenTest(unittest.TestCase):
    def test_deterministic_wire_bytes_match_frozen_fixture(self) -> None:
        fixture = json.loads(FIXTURE.read_text(encoding="utf-8"))
        self.assertEqual(1, fixture["schema_version"])
        for field, value in _fixture_payload().items():
            self.assertEqual(fixture[field], value, field)


if __name__ == "__main__":
    if sys.argv[1:] == ["--emit-fixture"]:
        print(json.dumps(_fixture_payload(), separators=(",", ":"), sort_keys=True))
        raise SystemExit(0)
    unittest.main()
