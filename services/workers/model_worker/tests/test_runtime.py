# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from __future__ import annotations

import asyncio
import hashlib
import json
import os
import stat
import struct
import tempfile
import time
from pathlib import Path
from threading import Event

import pytest
from mindclade.runtime.v1 import buffer_descriptor_pb2, worker_command_pb2, worker_status_pb2
from models.reference.affine_worker.make_bundle import generate
from services.workers.model_worker.config import WorkerProcessConfig
from services.workers.model_worker.ipc import MAX_FRAME_BYTES, WorkerServer
from serving.model_worker.reference import (
    ExecutionCancelled,
    ReferenceEngine,
    ReferenceEngineConfig,
    ReferenceInput,
    ReferenceRequest,
)


def _sha256(value: bytes) -> str:
    return "sha256:" + hashlib.sha256(value).hexdigest()


def _roots(tmp_path: Path) -> tuple[Path, Path, Path]:
    tmp_path.mkdir(parents=True, exist_ok=True)
    bundle = tmp_path / "bundle"
    inputs = tmp_path / "inputs"
    outputs = tmp_path / "outputs"
    inputs.mkdir()
    outputs.mkdir()
    generate(bundle)
    return bundle, inputs, outputs


def _process_config(
    bundle: Path,
    inputs: Path,
    outputs: Path,
    *,
    iterations: int = 1,
) -> WorkerProcessConfig:
    manifest = json.loads((bundle / "manifest.json").read_text())
    config = WorkerProcessConfig(
        schema_version=1,
        model_bundle_root=bundle,
        model_bundle_digest=manifest["digest"],
        output_root=outputs,
        allowed_input_roots=(inputs,),
        device="cpu",
        maximum_pending_requests=8,
        maximum_concurrent_executions=2,
        maximum_input_bytes=1 << 20,
        maximum_output_bytes=1 << 20,
        io_timeout_millis=5_000,
        cancellation_grace_millis=2_000,
        reference_chunk_elements=64,
        reference_iterations=iterations,
    )
    config.validate()
    return config


def _engine(config: WorkerProcessConfig) -> ReferenceEngine:
    return ReferenceEngine(
        ReferenceEngineConfig(
            model_bundle_root=config.model_bundle_root,
            expected_bundle_digest=config.model_bundle_digest,
            output_root=config.output_root,
            allowed_input_roots=config.allowed_input_roots,
            device=config.device,
            chunk_elements=config.reference_chunk_elements,
            iterations=config.reference_iterations,
        )
    )


def _request(config: WorkerProcessConfig, input_path: Path, payload: bytes) -> ReferenceRequest:
    deadline = int(time.time() * 1000) + 30_000
    return ReferenceRequest(
        request_id="request-1",
        model_bundle_digest=config.model_bundle_digest,
        operation="reference.affine.v1",
        deadline_unix_millis=deadline,
        maximum_input_bytes=config.maximum_input_bytes,
        maximum_output_bytes=config.maximum_output_bytes,
        input=ReferenceInput(
            segment_id="input-1",
            generation=1,
            path=input_path,
            offset_bytes=0,
            length_bytes=len(payload),
            element_type="f32",
            shape=(len(payload) // 4,),
            content_digest=_sha256(payload),
            lease_expires_unix_millis=deadline,
        ),
    )


def test_config_is_versioned_bounded_and_rejects_unknown_fields(tmp_path: Path) -> None:
    bundle, inputs, outputs = _roots(tmp_path)
    config = _process_config(bundle, inputs, outputs)
    document = {
        "schema_version": config.schema_version,
        "model_bundle_root": str(config.model_bundle_root),
        "model_bundle_digest": config.model_bundle_digest,
        "output_root": str(config.output_root),
        "allowed_input_roots": [str(root) for root in config.allowed_input_roots],
        "device": config.device,
        "maximum_pending_requests": config.maximum_pending_requests,
        "maximum_concurrent_executions": config.maximum_concurrent_executions,
        "maximum_input_bytes": config.maximum_input_bytes,
        "maximum_output_bytes": config.maximum_output_bytes,
        "io_timeout_millis": config.io_timeout_millis,
        "cancellation_grace_millis": config.cancellation_grace_millis,
        "reference_chunk_elements": config.reference_chunk_elements,
        "reference_iterations": config.reference_iterations,
    }
    path = tmp_path / "worker.json"
    path.write_text(json.dumps(document))
    assert WorkerProcessConfig.from_file(path) == config
    document["unexpected"] = True
    path.write_text(json.dumps(document))
    with pytest.raises(ValueError, match="fields are invalid"):
        WorkerProcessConfig.from_file(path)


def test_reference_engine_verifies_and_executes_affine_bundle(tmp_path: Path) -> None:
    bundle, inputs, outputs = _roots(tmp_path)
    config = _process_config(bundle, inputs, outputs)
    payload = struct.pack("<ff", 1.0, -1.0)
    input_path = inputs / "request.f32"
    input_path.write_bytes(payload)
    result = _engine(config).execute(_request(config, input_path, payload), Event())
    assert struct.unpack("<ff", result.path.read_bytes()) == (2.5, -1.5)
    assert result.content_digest == _sha256(result.path.read_bytes())
    assert stat.S_IMODE(result.path.stat().st_mode) == stat.S_IRUSR

    cancelled = Event()
    cancelled.set()
    with pytest.raises(ExecutionCancelled):
        _engine(config).execute(_request(config, input_path, payload), cancelled)


def test_reference_engine_rejects_tampered_bundle_and_input(tmp_path: Path) -> None:
    bundle, inputs, outputs = _roots(tmp_path)
    config = _process_config(bundle, inputs, outputs)
    (bundle / "model.safetensors").write_bytes(b"tampered")
    with pytest.raises(ValueError, match="failed verification"):
        _engine(config)

    bundle, inputs, outputs = _roots(tmp_path / "second")
    config = _process_config(bundle, inputs, outputs)
    payload = struct.pack("<f", 1.0)
    input_path = inputs / "request.f32"
    input_path.write_bytes(payload)
    request = _request(config, input_path, payload)
    input_path.write_bytes(struct.pack("<f", 2.0))
    with pytest.raises(ValueError, match="digest mismatch"):
        _engine(config).execute(request, Event())


async def _send(writer: asyncio.StreamWriter, command: worker_command_pb2.WorkerCommand) -> None:
    payload = command.SerializeToString(deterministic=True)
    writer.write(struct.pack(">I", len(payload)) + payload)
    await writer.drain()


async def _receive(reader: asyncio.StreamReader) -> worker_status_pb2.WorkerStatus:
    (length,) = struct.unpack(">I", await reader.readexactly(4))
    payload = await reader.readexactly(length)
    status_message = worker_status_pb2.WorkerStatus()
    status_message.ParseFromString(payload)
    return status_message


def _start_command(
    config: WorkerProcessConfig, input_path: Path, payload: bytes
) -> worker_command_pb2.WorkerCommand:
    now = int(time.time() * 1000)
    command = worker_command_pb2.WorkerCommand(sequence=1)
    start = command.start
    start.operation = "reference.affine.v1"
    claims = start.ticket.claims
    claims.ticket_id = "ticket-1"
    claims.request_id = "request-1"
    claims.fencing_token = 9
    claims.model_bundle_digest = config.model_bundle_digest
    claims.deadline_unix_millis = now + 30_000
    claims.expires_unix_millis = now + 30_000
    claims.budget.maximum_output_bytes = len(payload)
    claims.artifacts.maximum_read_bytes = len(payload)
    start.ticket.signature.algorithm = "ed25519"
    start.ticket.signature.key_id = "test-key"
    start.ticket.signature.value = b"verified-by-runtime-host"
    start.inputs.add(
        segment_id="input-1",
        generation=1,
        offset_bytes=0,
        length_bytes=len(payload),
        element_type="f32",
        shape=(len(payload) // 4,),
        content_digest=_sha256(payload),
        owner_process="runtime-host",
        lease_expires_unix_millis=now + 30_000,
        access_mode=buffer_descriptor_pb2.BufferDescriptor.ACCESS_MODE_READ_ONLY,
        transport=buffer_descriptor_pb2.BufferDescriptor.TRANSPORT_LOCAL_FILE,
        locator=str(input_path),
    )
    return command


async def _exercise_ipc(tmp_path: Path) -> None:
    bundle, inputs, outputs = _roots(tmp_path)
    config = _process_config(bundle, inputs, outputs)
    payload = struct.pack("<ff", 1.0, -1.0)
    input_path = inputs / "request.f32"
    input_path.write_bytes(payload)

    socket_parent = Path(tempfile.mkdtemp(prefix="mindclade-worker-", dir=Path("/tmp").resolve()))
    os.chmod(socket_parent, stat.S_IRWXU)
    socket_path = socket_parent / "worker.sock"
    server = WorkerServer(config, _engine(config))
    await server.start(socket_path)
    try:
        reader, writer = await asyncio.open_unix_connection(socket_path)
        await _send(writer, _start_command(config, input_path, payload))
        assert (await _receive(reader)).state == worker_status_pb2.WORKER_STATE_RUNNING
        completed = await _receive(reader)
        assert completed.state == worker_status_pb2.WORKER_STATE_COMPLETED
        assert len(completed.outputs) == 1
        assert struct.unpack("<ff", Path(completed.outputs[0].locator).read_bytes()) == (2.5, -1.5)
        writer.close()
        await writer.wait_closed()

        oversized_reader, oversized_writer = await asyncio.open_unix_connection(socket_path)
        oversized_writer.write(struct.pack(">I", MAX_FRAME_BYTES + 1))
        await oversized_writer.drain()
        assert await oversized_reader.read() == b""
        oversized_writer.close()
        await oversized_writer.wait_closed()
    finally:
        await server.close()
        socket_parent.rmdir()


def test_worker_ipc_completes_and_rejects_oversized_frames(tmp_path: Path) -> None:
    asyncio.run(_exercise_ipc(tmp_path))


async def _exercise_cancellation(tmp_path: Path) -> None:
    bundle, inputs, outputs = _roots(tmp_path)
    config = _process_config(bundle, inputs, outputs, iterations=1_000_000)
    payload = struct.pack("<f", 1.0)
    input_path = inputs / "request.f32"
    input_path.write_bytes(payload)
    socket_parent = Path(tempfile.mkdtemp(prefix="mindclade-worker-", dir=Path("/tmp").resolve()))
    os.chmod(socket_parent, stat.S_IRWXU)
    socket_path = socket_parent / "worker.sock"
    server = WorkerServer(config, _engine(config))
    await server.start(socket_path)
    try:
        reader, writer = await asyncio.open_unix_connection(socket_path)
        await _send(writer, _start_command(config, input_path, payload))
        assert (await _receive(reader)).state == worker_status_pb2.WORKER_STATE_RUNNING
        cancel = worker_command_pb2.WorkerCommand(sequence=2)
        cancel.cancel.reason = "client cancelled"
        cancel.cancel.deadline_unix_millis = int(time.time() * 1000) + 2_000
        await _send(writer, cancel)
        assert (await _receive(reader)).state == worker_status_pb2.WORKER_STATE_CANCELLED
        writer.close()
        await writer.wait_closed()
    finally:
        await server.close()
        socket_parent.rmdir()


def test_worker_ipc_acknowledges_cancellation_after_execution_stops(tmp_path: Path) -> None:
    asyncio.run(_exercise_cancellation(tmp_path))
