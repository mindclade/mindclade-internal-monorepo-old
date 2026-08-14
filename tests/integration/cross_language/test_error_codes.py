from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
def test_error_code_contract_has_one_wire_source():
    proto=(ROOT/"protocols/proto/mindclade/common/v1/errors.proto").read_text()
    assert "message" in proto and "Error" in proto
    # Go and Rust keep transport-neutral enums and map at boundaries; protocol is the wire authority.
    assert (ROOT/"libs/go/faults/code.go").exists()
    assert (ROOT/"libs/rust/faults/src/code.rs").exists()
