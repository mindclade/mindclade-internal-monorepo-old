from pathlib import Path
import importlib.util
import sys

MODULE = Path(__file__).resolve().parents[1] / 'affected.py'
spec = importlib.util.spec_from_file_location('affected', MODULE)
affected = importlib.util.module_from_spec(spec)
assert spec.loader
sys.modules[spec.name] = affected
spec.loader.exec_module(affected)

def test_global_changes_force_full_graph():
    assert affected.select(['Cargo.toml']) == ['//...']

def test_unknown_unowned_file_falls_back_full():
    assert affected.select(['definitely-not-a-package.txt']) == ['//...']


def test_rust_qualification_is_affected():
    assert affected.rust_qualification_required(["libs/rust/runtime_core/src/lib.rs"])
    assert affected.rust_qualification_required(["Cargo.toml"])
    assert not affected.rust_qualification_required(["control/artifacts/gc.go"])
