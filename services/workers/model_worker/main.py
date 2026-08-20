# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Production process entrypoint for the bounded Python/PyTorch worker."""

from __future__ import annotations

import asyncio
import contextlib
import os
import signal
from pathlib import Path

from serving.model_worker.reference import ReferenceEngine, ReferenceEngineConfig

from services.workers.model_worker.config import WorkerProcessConfig
from services.workers.model_worker.ipc import WorkerServer


async def _run(config: WorkerProcessConfig, socket_path: Path) -> None:
    engine = ReferenceEngine(
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
    server = WorkerServer(config, engine)
    await server.start(socket_path)
    stopped = asyncio.Event()
    loop = asyncio.get_running_loop()
    for signal_number in (signal.SIGINT, signal.SIGTERM):
        with contextlib.suppress(NotImplementedError):
            loop.add_signal_handler(signal_number, stopped.set)
    try:
        await stopped.wait()
    finally:
        await server.close()


def main() -> int:
    config_value = os.environ.get("MINDCLADE_MODEL_WORKER_CONFIG", "")
    socket_value = os.environ.get("MINDCLADE_MODEL_WORKER_SOCKET", "")
    if not config_value or not socket_value:
        raise SystemExit(
            "MINDCLADE_MODEL_WORKER_CONFIG and MINDCLADE_MODEL_WORKER_SOCKET are required"
        )
    config = WorkerProcessConfig.from_file(Path(config_value))
    asyncio.run(_run(config, Path(socket_value)))
    return 0


if __name__ == "__main__":
    main()
