from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def test_worker_protocol_has_control_and_bulk_descriptor_contracts():
    worker = (ROOT / "protocols/proto/mindclade/runtime/v1/worker_command.proto").read_text()
    buffer = (ROOT / "protocols/proto/mindclade/runtime/v1/buffer_descriptor.proto").read_text()
    assert "ExecutionTicket" in worker and "BufferDescriptor" in worker
    assert "content_digest" in buffer or "digest" in buffer
    assert "bytes payload" not in worker  # large payloads travel by descriptor, not protobuf bytes
