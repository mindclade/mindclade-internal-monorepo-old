from __future__ import annotations
import json,re
from pathlib import Path
F=Path(__file__).parent/"fixtures"/"primitives_v1.json"
def test_artifact_identity_is_location_independent():
    ref=json.loads(F.read_text())["artifact_ref"]
    assert set(ref)=={"digest","size_bytes","media_type","logical_kind","schema_version"}
    assert re.fullmatch(r"sha256:[0-9a-f]{64}",ref["digest"]);assert ref["size_bytes"]>=0
    assert "/" in ref["media_type"] and ref["logical_kind"] and ref["schema_version"]>0
    assert "uri" not in ref and "provider" not in ref and "generation" not in ref
