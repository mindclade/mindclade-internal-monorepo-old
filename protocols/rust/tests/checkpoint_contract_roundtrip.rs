// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_protocols::artifact::v1::CheckpointCommit;
use mindclade_protocols::common::v1::ArtifactRef;
use mindclade_protocols::registry::v1::CheckpointRecord;
use mindclade_protocols::training::v1::{CheckpointManifest, TrainingTopology};
use prost::Message;
use serde_json::{Map, Value};

const FIXTURE_JSON: &str =
    include_str!("../../../tests/integration/cross_language/fixtures/checkpoint_contract_v1.json");
const MAX_MESSAGE_BYTES: usize = 64 * 1024;

fn nibble(byte: u8) -> Option<u8> {
    match byte {
        b'0'..=b'9' => Some(byte - b'0'),
        b'a'..=b'f' => Some(byte - b'a' + 10),
        _ => None,
    }
}

fn decode_hex(value: &str) -> Result<Vec<u8>, &'static str> {
    if !value.len().is_multiple_of(2) {
        return Err("fixture hex has odd length");
    }
    if value.len() / 2 > MAX_MESSAGE_BYTES {
        return Err("fixture message exceeds the test bound");
    }
    value
        .as_bytes()
        .chunks_exact(2)
        .map(|pair| {
            let high = nibble(pair[0]).ok_or("fixture hex is not lowercase hexadecimal")?;
            let low = nibble(pair[1]).ok_or("fixture hex is not lowercase hexadecimal")?;
            Ok((high << 4) | low)
        })
        .collect()
}

fn fixture_bytes(fixture: &Map<String, Value>, field: &str) -> Vec<u8> {
    let encoded = fixture
        .get(field)
        .and_then(Value::as_str)
        .unwrap_or_else(|| panic!("{field} must be a string"));
    decode_hex(encoded).unwrap_or_else(|error| panic!("{field}: {error}"))
}

fn decode_exact<M>(fixture: &Map<String, Value>, field: &str) -> M
where
    M: Message + Default,
{
    let frozen = fixture_bytes(fixture, field);
    let message =
        M::decode(frozen.as_slice()).unwrap_or_else(|error| panic!("{field} must decode: {error}"));
    assert_eq!(message.encode_to_vec(), frozen, "{field}");
    message
}

fn assert_artifact(artifact: &ArtifactRef, seed: char, logical_kind: &str, size_bytes: u64) {
    assert_eq!(
        artifact.digest,
        format!("sha256:{}", seed.to_string().repeat(64))
    );
    assert_eq!(artifact.size_bytes, size_bytes);
    assert_eq!(
        artifact.media_type,
        format!(
            "application/vnd.mindclade.{}.v1+proto",
            logical_kind.replace('.', "-")
        )
    );
    assert_eq!(artifact.logical_kind, logical_kind);
    assert_eq!(artifact.schema_version, 1);
}

fn fixture() -> Value {
    let fixture: Value = serde_json::from_str(FIXTURE_JSON).expect("fixture must be valid JSON");
    let object = fixture.as_object().expect("fixture must be a JSON object");
    let mut fields: Vec<&str> = object.keys().map(String::as_str).collect();
    fields.sort_unstable();
    assert_eq!(
        fields,
        [
            "commit_hex",
            "manifest_hex",
            "record_hex",
            "schema_version",
            "topology_hex",
        ]
    );
    assert_eq!(
        object.get("schema_version").and_then(Value::as_u64),
        Some(1)
    );
    fixture
}

fn assert_topology(topology: &TrainingTopology) {
    assert_eq!(topology.world_size, 2);
    assert_eq!(topology.local_world_size, 2);
    assert_eq!(topology.node_count, 1);
    assert_eq!(topology.data_parallel_size, 2);
    assert_eq!(topology.tensor_parallel_size, 1);
    assert_eq!(topology.pipeline_parallel_size, 1);
    assert_eq!(topology.backend, 1);
    assert_eq!(topology.device_type, 1);
    assert_eq!(
        topology.topology_fingerprint,
        format!("sha256:{}", "a".repeat(64))
    );
}

fn assert_manifest(manifest: &CheckpointManifest, topology: &TrainingTopology) {
    assert_eq!(manifest.schema_version, 1);
    assert_eq!(
        manifest.checkpoint_id,
        "checkpoint_019c0000000070008000000000000001"
    );
    assert_eq!(manifest.run_id, "run_019c0000000070008000000000000002");
    assert_eq!(manifest.attempt_id, "attempt-2");
    assert_eq!(manifest.checkpoint_attempt, 2);
    assert_eq!(manifest.data_position, 32);
    let counters = manifest.counters.as_ref().expect("counters");
    assert_eq!(counters.microbatches, 8);
    assert_eq!(counters.optimizer_steps, 4);
    assert_eq!(counters.samples, 32);
    assert_eq!(manifest.topology.as_ref(), Some(topology));
    assert_artifact(
        manifest.resolved_config.as_ref().expect("resolved config"),
        '1',
        "training.resolved-config",
        128,
    );
    assert_artifact(
        manifest.dataset.as_ref().expect("dataset"),
        '2',
        "training.dataset",
        256,
    );
    assert_artifact(
        manifest.model.as_ref().expect("model"),
        '3',
        "training.model",
        64,
    );
    assert_artifact(
        manifest.source.as_ref().expect("source"),
        '4',
        "source.archive",
        512,
    );
    assert_artifact(
        manifest.toolchain.as_ref().expect("toolchain"),
        '5',
        "toolchain.manifest",
        128,
    );
    assert_artifact(
        manifest.environment.as_ref().expect("environment"),
        '6',
        "training.environment",
        128,
    );
    assert_artifact(
        manifest
            .compatibility_policy
            .as_ref()
            .expect("compatibility policy"),
        '8',
        "training.checkpoint.compatibility-policy",
        96,
    );
    assert!(manifest.parent_manifest.is_none());
    assert_eq!(manifest.components.len(), 1);
    let component = &manifest.components[0];
    assert_eq!(component.name, "model");
    assert_eq!(component.kind, 1);
    assert_eq!(component.relative_path, "rank-00000/model.safetensors");
    assert_eq!(component.rank, 0);
    assert_eq!(component.tensor_layout, 1);
    assert_eq!(component.tensor_fqns, ["bias", "scale"]);
    assert_artifact(
        component.artifact.as_ref().expect("component artifact"),
        '7',
        "training.checkpoint.tensors",
        1024,
    );
    let created_at = manifest.created_at.as_ref().expect("manifest created_at");
    assert_eq!(created_at.seconds, 1_800_000_000);
    assert_eq!(created_at.nanos, 0);
}

fn assert_commit(commit: &CheckpointCommit, manifest: &CheckpointManifest) {
    assert_eq!(commit.schema_version, 1);
    assert_eq!(commit.checkpoint_id, manifest.checkpoint_id);
    assert_eq!(commit.run_id, manifest.run_id);
    assert_eq!(commit.checkpoint_attempt, 2);
    assert_eq!(commit.fencing_token, 9);
    assert_eq!(commit.writer_id, "checkpoint-agent-0");
    assert_eq!(commit.commit_digest, format!("sha256:{}", "b".repeat(64)));
    assert_artifact(
        commit.manifest.as_ref().expect("commit manifest"),
        '9',
        "training.checkpoint.manifest",
        2048,
    );
    assert_eq!(
        commit.committed_at.as_ref().expect("committed_at").seconds,
        1_800_000_000
    );
}

fn assert_record(
    record: &CheckpointRecord,
    manifest: &CheckpointManifest,
    commit: &CheckpointCommit,
    topology: &TrainingTopology,
) {
    assert_eq!(record.schema_version, 1);
    assert_eq!(record.checkpoint_id, manifest.checkpoint_id);
    assert_eq!(record.run_id, manifest.run_id);
    assert_eq!(record.manifest, commit.manifest);
    assert_artifact(
        record.commit.as_ref().expect("registry commit"),
        'a',
        "artifact.checkpoint.commit",
        512,
    );
    assert_eq!(record.optimizer_step, 4);
    assert_eq!(record.checkpoint_attempt, 2);
    assert_eq!(record.fencing_token, 9);
    assert_eq!(record.topology_fingerprint, topology.topology_fingerprint);
    assert!(record.parent_manifest.is_none());
    assert_eq!(record.lifecycle, 1);
    assert_eq!(record.policy_epoch, 3);
    assert_eq!(record.resource_version, 1);
    assert_eq!(record.record_digest, format!("sha256:{}", "c".repeat(64)));
    assert_eq!(
        record
            .created_at
            .as_ref()
            .expect("record created_at")
            .seconds,
        1_800_000_000
    );
    assert_eq!(
        record
            .retain_until
            .as_ref()
            .expect("record retain_until")
            .seconds,
        1_800_086_400
    );
}

#[test]
fn canonical_checkpoint_messages_match_python_and_typescript_wire_bytes() {
    let fixture = fixture();
    let fixture = fixture.as_object().expect("validated fixture object");
    let topology: TrainingTopology = decode_exact(fixture, "topology_hex");
    let manifest: CheckpointManifest = decode_exact(fixture, "manifest_hex");
    let commit: CheckpointCommit = decode_exact(fixture, "commit_hex");
    let record: CheckpointRecord = decode_exact(fixture, "record_hex");

    assert_topology(&topology);
    assert_manifest(&manifest, &topology);
    assert_commit(&commit, &manifest);
    assert_record(&record, &manifest, &commit, &topology);
}
