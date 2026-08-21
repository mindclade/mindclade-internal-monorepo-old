#!/usr/bin/env python3
"""Two-process CPU DDP smoke test. Launch with torchrun."""

from __future__ import annotations

import os
from datetime import timedelta

try:
    import torch
    import torch.distributed as dist
    from torch.nn.parallel import DistributedDataParallel
except ImportError as exc:  # pragma: no cover - user environment diagnostic
    raise SystemExit(f"PyTorch is required: {exc}")


def main() -> int:
    required = ("RANK", "LOCAL_RANK", "WORLD_SIZE")
    missing = [name for name in required if name not in os.environ]
    if missing:
        raise SystemExit(
            "Launch with torchrun, for example: "
            "torchrun --standalone --nproc-per-node=2 scripts/ddp_smoke.py. "
            f"Missing: {', '.join(missing)}"
        )

    rank = int(os.environ["RANK"])
    world_size = int(os.environ["WORLD_SIZE"])
    if world_size < 2:
        raise SystemExit("This smoke test expects at least two processes.")
    if not dist.is_available() or not dist.is_gloo_available():
        raise SystemExit("This PyTorch build does not provide the Gloo backend.")

    dist.init_process_group("gloo", timeout=timedelta(seconds=60))
    try:
        torch.manual_seed(0)
        model = torch.nn.Linear(4, 2)
        ddp = DistributedDataParallel(model)
        optimizer = torch.optim.SGD(ddp.parameters(), lr=0.1)

        x = torch.full((3, 4), float(rank + 1))
        target = torch.zeros((3, 2))
        optimizer.zero_grad(set_to_none=True)
        loss = torch.nn.functional.mse_loss(ddp(x), target)
        loss.backward()
        optimizer.step()

        reduced = loss.detach().clone()
        dist.all_reduce(reduced, op=dist.ReduceOp.SUM)
        mean_loss = reduced / world_size

        checksum = sum(parameter.detach().sum() for parameter in ddp.module.parameters())
        checksums = [torch.zeros_like(checksum) for _ in range(world_size)]
        dist.all_gather(checksums, checksum)
        if not all(torch.equal(checksums[0], value) for value in checksums[1:]):
            raise RuntimeError(f"Parameter checksums differ across ranks: {checksums}")

        if rank == 0:
            print(
                f"DDP smoke test passed: world_size={world_size}, "
                f"mean_loss={mean_loss.item():.6f}, checksum={checksum.item():.6f}"
            )
        dist.barrier()
    finally:
        dist.destroy_process_group()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
