"""Architecture contracts for the four production vertical slices."""

from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def assert_contains(path: str, *tokens: str) -> None:
    text = (ROOT / path).read_text()
    for token in tokens:
        assert token in text, f"{path} does not consume {token}"


def test_ingestion_to_dataset_slice() -> None:
    assert_contains(
        "services/workers/ingestion/src/executor.rs",
        "ExecutionTicket",
        "WorkerRuntime",
    )
    assert_contains("control/ingestion/state.go", "package ingestion")
    assert_contains("data/curation/pipeline.py", "CurationPipeline", "manifest_digest")


def test_preprocessing_to_structure_inference_slice() -> None:
    assert_contains(
        "preprocessing/pipeline/planner.py", "MSA_SEARCH", "TEMPLATE_SEARCH", "FEATURIZE"
    )
    assert_contains("preprocessing/provenance/manifest.py", "digest")
    assert_contains("serving/contracts/batch.py", "BatchPlan", "BatchPlanner")
    assert_contains("services/workers/model_worker/executor.py", "ModelEngine", "ModelWorker")


def test_gateway_host_python_slice() -> None:
    assert_contains(
        "services/runtime_gateway/src/network.rs",
        "RuntimeDispatchRequest",
        "GatewayCore",
        "ConcurrencyLimitLayer",
    )
    assert_contains(
        "services/runtime_gateway/src/authority.rs",
        "Ed25519KeySet",
        "PolicyCache",
    )
    assert_contains(
        "services/runtime_host/src/async_ipc.rs",
        "UnixListener",
        "MAX_FRAME_BYTES",
    )
    assert_contains(
        "services/runtime_host/src/bulk.rs",
        "BulkSegment",
        "FileSegment",
        "BufferDescriptor",
    )
    assert_contains(
        "services/runtime_host/src/authority.rs",
        "Ed25519KeySet",
        "begin_invocation",
    )
    assert_contains("services/workers/model_worker/README.md", "Python")


def test_training_to_release_slice() -> None:
    assert_contains("training/checkpointing/manager.py", "checkpoint")
    assert_contains("evaluation/harness/runner.py", "evaluation")
    assert_contains("control/registry/releases/model.go", "EvidenceGraph", "EvidenceRuntimeBundle")
