// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { create, toBinary } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { CheckpointCommitSchema } from "../src/generated/proto/mindclade/artifact/v1/checkpoint_pb.js";
import { ArtifactRefSchema } from "../src/generated/proto/mindclade/common/v1/artifact_ref_pb.js";
import {
  CheckpointLifecycle,
  CheckpointRecordSchema,
} from "../src/generated/proto/mindclade/registry/v1/checkpoint_pb.js";
import {
  CheckpointComponentKind,
  CheckpointComponentSchema,
  CheckpointManifestSchema,
  CheckpointTensorLayout,
} from "../src/generated/proto/mindclade/training/v1/checkpoint_pb.js";
import { TrainingCountersSchema } from "../src/generated/proto/mindclade/training/v1/progress_pb.js";
import {
  CollectiveBackend,
  TrainingDeviceType,
  TrainingTopologySchema,
} from "../src/generated/proto/mindclade/training/v1/topology_pb.js";

const fixture = JSON.parse(
  readFileSync("../../tests/integration/cross_language/fixtures/checkpoint_contract_v1.json", "utf8"),
) as Record<string, string | number>;

const timestamp = create(TimestampSchema, { seconds: 1800000000n, nanos: 0 });

function artifact(seed: string, logicalKind: string, sizeBytes: number) {
  return create(ArtifactRefSchema, {
    digest: `sha256:${seed.repeat(64)}`,
    sizeBytes: BigInt(sizeBytes),
    mediaType: `application/vnd.mindclade.${logicalKind.replaceAll(".", "-")}.v1+proto`,
    logicalKind,
    schemaVersion: 1,
  });
}

const topology = create(TrainingTopologySchema, {
  worldSize: 2,
  localWorldSize: 2,
  nodeCount: 1,
  dataParallelSize: 2,
  tensorParallelSize: 1,
  pipelineParallelSize: 1,
  backend: CollectiveBackend.GLOO,
  deviceType: TrainingDeviceType.CPU,
  topologyFingerprint: `sha256:${"a".repeat(64)}`,
});

const manifest = create(CheckpointManifestSchema, {
  schemaVersion: 1,
  checkpointId: "checkpoint_019c0000000070008000000000000001",
  runId: "run_019c0000000070008000000000000002",
  attemptId: "attempt-2",
  checkpointAttempt: 2n,
  counters: create(TrainingCountersSchema, {
    microbatches: 8n,
    optimizerSteps: 4n,
    samples: 32n,
  }),
  dataPosition: 32n,
  resolvedConfig: artifact("1", "training.resolved-config", 128),
  dataset: artifact("2", "training.dataset", 256),
  model: artifact("3", "training.model", 64),
  source: artifact("4", "source.archive", 512),
  toolchain: artifact("5", "toolchain.manifest", 128),
  environment: artifact("6", "training.environment", 128),
  topology,
  components: [
    create(CheckpointComponentSchema, {
      name: "model",
      kind: CheckpointComponentKind.MODEL,
      artifact: artifact("7", "training.checkpoint.tensors", 1024),
      relativePath: "rank-00000/model.safetensors",
      rank: 0,
      tensorLayout: CheckpointTensorLayout.REPLICATED,
      tensorFqns: ["bias", "scale"],
    }),
  ],
  compatibilityPolicy: artifact("8", "training.checkpoint.compatibility-policy", 96),
  createdAt: timestamp,
});

const commit = create(CheckpointCommitSchema, {
  schemaVersion: 1,
  checkpointId: manifest.checkpointId,
  runId: manifest.runId,
  manifest: artifact("9", "training.checkpoint.manifest", 2048),
  checkpointAttempt: 2n,
  fencingToken: 9n,
  writerId: "checkpoint-agent-0",
  committedAt: timestamp,
  commitDigest: `sha256:${"b".repeat(64)}`,
});

const record = create(CheckpointRecordSchema, {
  schemaVersion: 1,
  checkpointId: manifest.checkpointId,
  runId: manifest.runId,
  manifest: commit.manifest,
  commit: artifact("a", "artifact.checkpoint.commit", 512),
  optimizerStep: 4n,
  checkpointAttempt: 2n,
  fencingToken: 9n,
  topologyFingerprint: topology.topologyFingerprint,
  lifecycle: CheckpointLifecycle.COMMITTED,
  createdAt: timestamp,
  retainUntil: create(TimestampSchema, { seconds: 1800086400n, nanos: 0 }),
  policyEpoch: 3n,
  resourceVersion: 1n,
  recordDigest: `sha256:${"c".repeat(64)}`,
});

function hex(schema: Parameters<typeof toBinary>[0], message: Parameters<typeof toBinary>[1]) {
  return Buffer.from(toBinary(schema, message)).toString("hex");
}

test("checkpoint contracts match the frozen Python protobuf bytes", () => {
  assert.equal(fixture.schema_version, 1);
  assert.equal(hex(TrainingTopologySchema, topology), fixture.topology_hex);
  assert.equal(hex(CheckpointManifestSchema, manifest), fixture.manifest_hex);
  assert.equal(hex(CheckpointCommitSchema, commit), fixture.commit_hex);
  assert.equal(hex(CheckpointRecordSchema, record), fixture.record_hex);
});
