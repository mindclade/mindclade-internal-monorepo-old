from libs.python.worker_runtime import StageEnvelope, StageKind, WorkloadEnvelope

D = "sha256:" + "1" * 64


def rid(kind, n):
    return f"{kind}_019c00000000700080000000000000{n:02x}"


def test_workload_envelope_delegates_scientific_stage_validation():
    stage = StageEnvelope(
        stage_id=rid("stage", 1),
        kind=StageKind.PREPROCESS,
        operation="features",
        inputs=(),
        output_namespace="tenant/a",
        resolved_config_digest=D,
        reference_snapshot_digest=None,
        attempt=1,
        fencing_token=7,
        deadline_unix_millis=5000,
    )
    WorkloadEnvelope(
        rid("workload", 2),
        rid("run", 3),
        rid("job", 4),
        rid("tenant", 5),
        rid("workspace", 6),
        rid("ticket", 7),
        stage,
        "cpu-highmem",
        1000,
    ).validate()
