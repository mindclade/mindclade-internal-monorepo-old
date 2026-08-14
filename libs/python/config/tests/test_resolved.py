from pathlib import Path
import tempfile
from libs.python.config import resolve

def test_resolved_digest_stable_and_override_recorded():
    with tempfile.TemporaryDirectory() as d:
        a=Path(d)/"base.toml"; b=Path(d)/"model.toml"
        a.write_text("[runtime]\nprecision='bf16'\n[model]\nlayers=2\n")
        b.write_text("[model]\nwidth=64\n")
        x=resolve([a,b],overrides=["model.layers=4"]); y=resolve([a,b],overrides=["model.layers=4"] )
        assert x.digest==y.digest and x.value["model"]["layers"]==4
        assert x.overrides[0].path=="model.layers"
