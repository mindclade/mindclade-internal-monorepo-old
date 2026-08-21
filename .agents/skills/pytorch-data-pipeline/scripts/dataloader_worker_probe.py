#!/usr/bin/env python3
"""Exercise DataLoader worker launch and report worker seeds using synthetic data."""

from __future__ import annotations

import argparse
import json
from typing import Any

try:
    import torch
    from torch.utils.data import DataLoader, Dataset, get_worker_info
except ImportError as exc:  # pragma: no cover - user environment diagnostic
    raise SystemExit(f"PyTorch is required: {exc}")


class ProbeDataset(Dataset[dict[str, Any]]):
    def __init__(self, length: int) -> None:
        self.length = length

    def __len__(self) -> int:
        return self.length

    def __getitem__(self, index: int) -> dict[str, Any]:
        info = get_worker_info()
        worker_id = -1 if info is None else info.id
        worker_seed = torch.initial_seed()
        return {
            "index": index,
            "worker_id": worker_id,
            "worker_seed": str(worker_seed),
            "random_value": torch.rand(()).item(),
        }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--workers", type=int, default=2)
    parser.add_argument("--batches", type=int, default=3)
    parser.add_argument("--batch-size", type=int, default=4)
    parser.add_argument("--seed", type=int, default=1234)
    args = parser.parse_args()

    if args.workers < 0 or args.batches <= 0 or args.batch_size <= 0:
        parser.error("workers must be non-negative; batches and batch-size must be positive")

    generator = torch.Generator().manual_seed(args.seed)
    dataset = ProbeDataset(args.batches * args.batch_size)
    loader = DataLoader(
        dataset,
        batch_size=args.batch_size,
        num_workers=args.workers,
        generator=generator,
        shuffle=False,
        persistent_workers=args.workers > 0,
    )

    rows: list[dict[str, Any]] = []
    for batch_number, batch in enumerate(loader):
        rows.append(
            {
                "batch": batch_number,
                "indices": batch["index"].tolist(),
                "worker_ids": batch["worker_id"].tolist(),
                "worker_seeds": list(batch["worker_seed"]),
                "random_values": batch["random_value"].tolist(),
            }
        )
        if batch_number + 1 >= args.batches:
            break

    print(json.dumps({"torch": torch.__version__, "rows": rows}, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
