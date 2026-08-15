from __future__ import annotations

import hashlib
import hmac
import json
import struct
from pathlib import Path

ROOT = Path(__file__).parent / "fixtures"


def doc(kind: str):
    return bytearray(b"MCCE1/" + kind.encode() + b"\0")


def field(out: bytearray, key: str, value: bytes):
    out.extend(struct.pack(">H", len(key)))
    out.extend(key.encode())
    out.extend(struct.pack(">I", len(value)))
    out.extend(value)


def text(o, k, v):
    field(o, k, v.encode())


def u32(o, k, v):
    field(o, k, struct.pack(">I", v))


def u64(o, k, v):
    field(o, k, struct.pack(">Q", v))


def boolean(o, k, v):
    field(o, k, bytes([1 if v else 0]))


def strings(o, k, values):
    b = bytearray()
    values = sorted(values)
    b.extend(struct.pack(">I", len(values)))
    for v in values:
        b.extend(struct.pack(">I", len(v)))
        b.extend(v.encode())
    field(o, k, b)


def artifact_grant():
    o = doc("artifact-grant")
    strings(o, "readable_digests", ["sha256:" + "a" * 64])
    strings(o, "writable_namespaces", ["tenant/t1/run/r1"])
    u64(o, "maximum_read_bytes", 1024)
    u64(o, "maximum_write_bytes", 2048)
    boolean(o, "allow_range_reads", True)
    boolean(o, "allow_multipart_writes", True)
    return bytes(o)


def budget():
    o = doc("execution-budget")
    u32(o, "cpu_millis", 2000)
    u64(o, "resident_memory_bytes", 8 << 30)
    u64(o, "pinned_memory_bytes", 1 << 30)
    u64(o, "shared_memory_bytes", 512 << 20)
    u64(o, "local_disk_bytes", 16 << 30)
    u32(o, "open_file_descriptors", 128)
    u32(o, "object_store_requests", 16)
    u32(o, "queued_operations", 8)
    u32(o, "child_processes", 2)
    u32(o, "cpu_worker_threads", 8)
    u64(o, "gpu_memory_estimate_bytes", 40 << 30)
    u64(o, "checkpoint_staging_bytes", 4 << 30)
    u64(o, "telemetry_spool_bytes", 64 << 20)
    u64(o, "maximum_output_bytes", 2 << 30)
    return bytes(o)


def claims():
    o = doc("execution-ticket-claims")
    for k, v in [
        ("ticket_id", "ticket_019c0000000070008000000000000001"),
        ("issuer", "control"),
        ("tenant_id", "tenant_019c0000000070008000000000000002"),
        ("workspace_id", "workspace_019c0000000070008000000000000003"),
        ("run_id", ""),
        ("job_id", ""),
        ("stage_id", "stage_019c0000000070008000000000000004"),
        ("request_id", ""),
    ]:
        text(o, k, v)
    u32(o, "attempt", 1)
    u64(o, "fencing_token", 9)
    for k, ch in [
        ("model_bundle_digest", "1"),
        ("engine_bundle_digest", "2"),
        ("resolved_config_digest", "3"),
        ("reference_snapshot_digest", "4"),
    ]:
        text(o, k, "sha256:" + ch * 64)
    field(o, "artifact_grant", artifact_grant())
    field(o, "budget", budget())
    text(o, "execution_class", "gpu")
    text(o, "accelerator_capability", "sm90")
    u64(o, "not_before_unix_millis", 1800000000000)
    u64(o, "deadline_unix_millis", 1800000600000)
    u64(o, "expires_unix_millis", 1800000300000)
    u64(o, "policy_epoch", 12)
    u64(o, "route_snapshot_version", 34)
    u64(o, "revocation_epoch", 7)
    text(o, "idempotency_key", "run:r1:stage:s1:attempt:1")
    return bytes(o)


def test_go_and_python_mcce1_bytes_and_hmac_match():
    expected = (ROOT / "execution_ticket_claims_v1.bin").read_bytes()
    actual = claims()
    assert actual == expected
    meta = json.loads((ROOT / "execution_ticket_golden_v1.json").read_text())
    assert "sha256:" + hashlib.sha256(actual).hexdigest() == meta["claims_sha256"]
    key = bytes.fromhex(meta["hmac_key_hex"])
    assert hmac.new(key, actual, hashlib.sha256).hexdigest() == meta["signature_hex"]
