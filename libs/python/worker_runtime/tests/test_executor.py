from libs.python.worker_runtime import ArtifactRef, StageEnvelope, StageExecutor, StageKind, StageResult

D = "sha256:" + "1" * 64
SID = "stage_01890f2c7b7a70008000000000000000"

class Engine:
    def execute(self, stage: StageEnvelope) -> StageResult:
        return StageResult(outputs=(ArtifactRef(D, 4, "application/octet-stream", "features", "v1"),), metrics={"items": 1.0})

def test_stage_executor_enforces_kind_deadline_and_artifacts():
    stage = StageEnvelope(stage_id=SID, kind=StageKind.PREPROCESS, operation="features", inputs=(), output_namespace="tenant/a", resolved_config_digest=D, reference_snapshot_digest=None, attempt=1, fencing_token=7, deadline_unix_millis=5000)
    result = StageExecutor(StageKind.PREPROCESS, Engine(), now_millis=lambda: 1000).execute(stage)
    assert result.outputs[0].logical_kind == "features"
