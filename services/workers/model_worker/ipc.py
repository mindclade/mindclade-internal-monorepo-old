# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded length-prefixed protobuf transport for the Python model worker."""

from __future__ import annotations

import asyncio
import contextlib
import os
import stat
import struct
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from threading import Event

from google.protobuf.message import DecodeError
from mindclade.runtime.v1 import buffer_descriptor_pb2, worker_command_pb2, worker_status_pb2

from serving.model_worker.reference import (
    ExecutionCancelled,
    ReferenceEngine,
    ReferenceInput,
    ReferenceOutput,
    ReferenceRequest,
)

from .config import WorkerProcessConfig

MAX_FRAME_BYTES = 1 << 20
MAX_UNIX_SOCKET_PATH_BYTES = 100


class ProtocolError(ValueError):
    """Raised when a peer violates the local worker protocol."""


class WorkerServer:
    def __init__(self, config: WorkerProcessConfig, engine: ReferenceEngine) -> None:
        self._config = config
        self._engine = engine
        self._pending = asyncio.Semaphore(config.maximum_pending_requests)
        self._executor = ThreadPoolExecutor(
            max_workers=config.maximum_concurrent_executions,
            thread_name_prefix="model-execution",
        )
        self._listener: asyncio.AbstractServer | None = None
        self._socket_path: Path | None = None
        self._tasks: set[asyncio.Task[None]] = set()
        self._cancellations: set[Event] = set()

    async def start(self, socket_path: Path) -> None:
        _prepare_socket_path(socket_path)
        self._socket_path = socket_path
        self._listener = await asyncio.start_unix_server(
            self._spawn_client,
            path=socket_path,
            backlog=min(self._config.maximum_pending_requests, 128),
            limit=MAX_FRAME_BYTES + 4,
        )
        os.chmod(socket_path, stat.S_IRUSR | stat.S_IWUSR)

    async def close(self) -> None:
        for cancellation in tuple(self._cancellations):
            cancellation.set()
        if self._listener is not None:
            self._listener.close()
            await self._listener.wait_closed()
            self._listener = None
        if self._tasks:
            timeout_seconds = self._config.cancellation_grace_millis / 1000
            _, pending = await asyncio.wait(tuple(self._tasks), timeout=timeout_seconds)
            for task in pending:
                task.cancel()
            if pending:
                await asyncio.gather(*pending, return_exceptions=True)
        self._executor.shutdown(wait=False, cancel_futures=True)
        if self._socket_path is not None:
            _remove_owned_socket(self._socket_path)
            self._socket_path = None

    def _spawn_client(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        task = asyncio.create_task(self._handle_client_guarded(reader, writer))
        self._tasks.add(task)
        task.add_done_callback(self._tasks.discard)

    async def _handle_client_guarded(
        self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter
    ) -> None:
        try:
            async with self._pending:
                await self._handle_client(reader, writer)
        except (ConnectionError, asyncio.IncompleteReadError, TimeoutError, ProtocolError):
            pass
        finally:
            writer.close()
            with contextlib.suppress(ConnectionError):
                await writer.wait_closed()

    async def _handle_client(
        self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter
    ) -> None:
        first = await read_command(reader, self._config.io_timeout_millis)
        if first is None or first.sequence == 0 or first.WhichOneof("command") != "start":
            raise ProtocolError("first worker command must be a non-zero start command")
        start = first.start
        request = _reference_request(start, self._config)
        ticket_id = start.ticket.claims.ticket_id
        fencing_token = start.ticket.claims.fencing_token
        sequence = 1
        await write_status(
            writer,
            _status(
                sequence,
                ticket_id,
                fencing_token,
                worker_status_pb2.WORKER_STATE_RUNNING,
                "execution running",
            ),
            self._config.io_timeout_millis,
        )

        cancellation = Event()
        self._cancellations.add(cancellation)
        loop = asyncio.get_running_loop()
        execution = loop.run_in_executor(self._executor, self._engine.execute, request, cancellation)
        command = asyncio.create_task(read_command(reader, self._config.io_timeout_millis))
        last_command_sequence = first.sequence
        cancellation_requested = False
        try:
            while True:
                remaining = max(0.0, (request.deadline_unix_millis - _unix_millis()) / 1000)
                if remaining == 0:
                    cancellation.set()
                    cancellation_requested = True
                    if not await _stops_within(execution, self._config.cancellation_grace_millis):
                        return
                    break
                done, _ = await asyncio.wait(
                    {execution, command},
                    timeout=remaining,
                    return_when=asyncio.FIRST_COMPLETED,
                )
                if not done:
                    cancellation.set()
                    cancellation_requested = True
                    if not await _stops_within(execution, self._config.cancellation_grace_millis):
                        return
                    break
                if execution in done:
                    break

                incoming = command.result()
                if incoming is None:
                    cancellation.set()
                    await _stops_within(execution, self._config.cancellation_grace_millis)
                    return
                if incoming.sequence <= last_command_sequence:
                    cancellation.set()
                    if not await _stops_within(execution, self._config.cancellation_grace_millis):
                        return
                    sequence += 1
                    await write_status(
                        writer,
                        _status(
                            sequence,
                            ticket_id,
                            fencing_token,
                            worker_status_pb2.WORKER_STATE_FAILED,
                            "invalid command sequence",
                        ),
                        self._config.io_timeout_millis,
                    )
                    return
                last_command_sequence = incoming.sequence
                kind = incoming.WhichOneof("command")
                if kind == "heartbeat":
                    sequence += 1
                    await write_status(
                        writer,
                        _status(
                            sequence,
                            ticket_id,
                            fencing_token,
                            worker_status_pb2.WORKER_STATE_RUNNING,
                            "execution running",
                        ),
                        self._config.io_timeout_millis,
                    )
                    command = asyncio.create_task(read_command(reader, self._config.io_timeout_millis))
                    continue
                if kind == "drain":
                    sequence += 1
                    await write_status(
                        writer,
                        _status(
                            sequence,
                            ticket_id,
                            fencing_token,
                            worker_status_pb2.WORKER_STATE_DRAINING,
                            "execution draining",
                        ),
                        self._config.io_timeout_millis,
                    )
                if kind in {"cancel", "drain"}:
                    cancellation.set()
                    cancellation_requested = True
                    if not await _stops_within(execution, self._config.cancellation_grace_millis):
                        return
                    break

                cancellation.set()
                if not await _stops_within(execution, self._config.cancellation_grace_millis):
                    return
                sequence += 1
                await write_status(
                    writer,
                    _status(
                        sequence,
                        ticket_id,
                        fencing_token,
                        worker_status_pb2.WORKER_STATE_FAILED,
                        "unsupported worker command",
                    ),
                    self._config.io_timeout_millis,
                )
                return

            command.cancel()
            with contextlib.suppress(asyncio.CancelledError, ConnectionError, ProtocolError):
                await command
            sequence += 1
            try:
                output = execution.result()
            except ExecutionCancelled:
                cancellation_requested = True
                output = None
            except Exception:
                await write_status(
                    writer,
                    _status(
                        sequence,
                        ticket_id,
                        fencing_token,
                        worker_status_pb2.WORKER_STATE_FAILED,
                        "reference execution failed",
                    ),
                    self._config.io_timeout_millis,
                )
                return
            if cancellation_requested:
                if output is not None:
                    output.path.unlink(missing_ok=True)
                await write_status(
                    writer,
                    _status(
                        sequence,
                        ticket_id,
                        fencing_token,
                        worker_status_pb2.WORKER_STATE_CANCELLED,
                        "execution cancelled",
                    ),
                    self._config.io_timeout_millis,
                )
                return
            await write_status(
                writer,
                _status(
                    sequence,
                    ticket_id,
                    fencing_token,
                    worker_status_pb2.WORKER_STATE_COMPLETED,
                    "execution completed",
                    output,
                ),
                self._config.io_timeout_millis,
            )
        finally:
            cancellation.set()
            self._cancellations.discard(cancellation)
            if not command.done():
                command.cancel()


async def read_command(
    reader: asyncio.StreamReader, timeout_millis: int
) -> worker_command_pb2.WorkerCommand | None:
    timeout_seconds = timeout_millis / 1000
    try:
        prefix = await asyncio.wait_for(reader.readexactly(4), timeout_seconds)
    except asyncio.IncompleteReadError as error:
        if not error.partial:
            return None
        raise ProtocolError("truncated worker frame prefix") from error
    (frame_size,) = struct.unpack(">I", prefix)
    if frame_size == 0 or frame_size > MAX_FRAME_BYTES:
        raise ProtocolError("worker command frame size is outside bounds")
    payload = await asyncio.wait_for(reader.readexactly(frame_size), timeout_seconds)
    command = worker_command_pb2.WorkerCommand()
    try:
        command.ParseFromString(payload)
    except DecodeError as error:
        raise ProtocolError("worker command protobuf is invalid") from error
    return command


async def write_status(
    writer: asyncio.StreamWriter,
    status_message: worker_status_pb2.WorkerStatus,
    timeout_millis: int,
) -> None:
    payload = status_message.SerializeToString(deterministic=True)
    if not payload or len(payload) > MAX_FRAME_BYTES:
        raise ProtocolError("worker status frame size is outside bounds")
    writer.write(struct.pack(">I", len(payload)) + payload)
    await asyncio.wait_for(writer.drain(), timeout_millis / 1000)


def _reference_request(
    start: worker_command_pb2.StartCommand, config: WorkerProcessConfig
) -> ReferenceRequest:
    if not start.HasField("ticket") or not start.ticket.HasField("claims"):
        raise ProtocolError("start command is missing execution ticket claims")
    ticket = start.ticket
    claims = ticket.claims
    signature = ticket.signature
    if not signature.algorithm or not signature.key_id or not signature.value:
        raise ProtocolError("execution ticket is missing its verified signature")
    now = _unix_millis()
    if (
        not claims.ticket_id
        or claims.fencing_token == 0
        or claims.deadline_unix_millis <= now
        or claims.expires_unix_millis <= now
        or claims.expires_unix_millis > claims.deadline_unix_millis
    ):
        raise ProtocolError("execution ticket identity or deadline is invalid")
    if claims.model_bundle_digest != config.model_bundle_digest:
        raise ProtocolError("execution ticket model digest does not match loaded worker")
    signed_output_limit = claims.budget.maximum_output_bytes
    if signed_output_limit == 0 or signed_output_limit > config.maximum_output_bytes:
        raise ProtocolError("execution output budget is outside worker bounds")
    if len(start.inputs) != 1:
        raise ProtocolError("reference operation requires exactly one input")
    descriptor = start.inputs[0]
    if (
        descriptor.access_mode != buffer_descriptor_pb2.BufferDescriptor.ACCESS_MODE_READ_ONLY
        or descriptor.transport
        not in {
            buffer_descriptor_pb2.BufferDescriptor.TRANSPORT_LOCAL_FILE,
            buffer_descriptor_pb2.BufferDescriptor.TRANSPORT_SHARED_MEMORY,
        }
        or descriptor.generation == 0
        or not descriptor.segment_id
        or not descriptor.content_digest
        or not descriptor.locator
        or descriptor.lease_expires_unix_millis <= now
    ):
        raise ProtocolError("reference input descriptor is invalid")
    maximum_read = claims.artifacts.maximum_read_bytes
    maximum_input = min(config.maximum_input_bytes, maximum_read or config.maximum_input_bytes)
    return ReferenceRequest(
        request_id=claims.request_id or claims.ticket_id,
        model_bundle_digest=claims.model_bundle_digest,
        operation=start.operation,
        deadline_unix_millis=claims.deadline_unix_millis,
        maximum_input_bytes=maximum_input,
        maximum_output_bytes=signed_output_limit,
        input=ReferenceInput(
            segment_id=descriptor.segment_id,
            generation=descriptor.generation,
            path=Path(descriptor.locator),
            offset_bytes=descriptor.offset_bytes,
            length_bytes=descriptor.length_bytes,
            element_type=descriptor.element_type,
            shape=tuple(descriptor.shape),
            content_digest=descriptor.content_digest,
            lease_expires_unix_millis=descriptor.lease_expires_unix_millis,
        ),
    )


def _status(
    sequence: int,
    ticket_id: str,
    fencing_token: int,
    state: int,
    message: str,
    output: ReferenceOutput | None = None,
) -> worker_status_pb2.WorkerStatus:
    status_message = worker_status_pb2.WorkerStatus(
        sequence=sequence,
        ticket_id=ticket_id,
        fencing_token=fencing_token,
        state=state,
        observed_unix_millis=_unix_millis(),
        message=message,
    )
    if output is not None:
        status_message.outputs.add(
            segment_id=output.segment_id,
            generation=output.generation,
            offset_bytes=0,
            length_bytes=output.length_bytes,
            element_type=output.element_type,
            shape=output.shape,
            content_digest=output.content_digest,
            owner_process="model-worker",
            lease_expires_unix_millis=output.lease_expires_unix_millis,
            access_mode=buffer_descriptor_pb2.BufferDescriptor.ACCESS_MODE_READ_ONLY,
            transport=buffer_descriptor_pb2.BufferDescriptor.TRANSPORT_LOCAL_FILE,
            locator=str(output.path),
        )
    return status_message


async def _stops_within(execution: asyncio.Future[ReferenceOutput], grace_millis: int) -> bool:
    try:
        await asyncio.wait_for(asyncio.shield(execution), grace_millis / 1000)
    except Exception:
        return execution.done()
    return True


def _prepare_socket_path(path: Path) -> None:
    if not path.is_absolute() or len(os.fsencode(path)) > MAX_UNIX_SOCKET_PATH_BYTES:
        raise ValueError("model-worker socket path must be short and absolute")
    parent = path.parent
    if parent.is_symlink() or parent.resolve(strict=True) != parent:
        raise ValueError("model-worker socket parent must be canonical and not a symlink")
    parent_stat = parent.stat()
    if parent_stat.st_uid != os.geteuid() or parent_stat.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        raise ValueError(
            "model-worker socket parent must be owned by the worker and not writable by peers"
        )
    if path.exists() or path.is_symlink():
        _remove_owned_socket(path)


def _remove_owned_socket(path: Path) -> None:
    try:
        path_stat = path.lstat()
    except FileNotFoundError:
        return
    if path_stat.st_uid != os.geteuid() or not stat.S_ISSOCK(path_stat.st_mode):
        raise ValueError("refusing to remove a model-worker path that is not an owned socket")
    path.unlink()


def _unix_millis() -> int:
    return int(time.time() * 1000)
