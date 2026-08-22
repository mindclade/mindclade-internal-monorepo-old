# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Two-rank worker launched only by the owning distributed smoke test."""

from __future__ import annotations

import json
import os
import sys
from collections.abc import Mapping
from pathlib import Path

import torch

from libs.python.errors import Canceled, FailedPrecondition, InvalidArgument
from libs.python.identifiers import Digest
from models.reference import ReferenceAffine
from training.checkpointing import (
    CheckpointIdentity,
    restore_distributed_checkpoint,
    save_distributed_checkpoint,
)
from training.checkpointing import dcp as dcp_module
from training.checkpointing.atomic_commit import read_checkpoint_member
from training.checkpointing.serialization import encode_state_component
from training.contracts import SupervisedBatch, TrainingState
from training.core import Trainer, TrainerConfig
from training.distributed import (
    DistributedConfig,
    DistributedContext,
    distributed_session,
    shard_supervised_batch,
)
from training.distributed.communication import DDPReducer
from training.distributed.parallelism import wrap_ddp
from training.tasks import SupervisedMSETask


def identity(topology: str) -> CheckpointIdentity:
    if topology.endswith("2"):
        checkpoint_id = "checkpoint_018bcfe8754073febc5bc0c0db31a415"
        run_id = "run_018bcfe875417430ac4115e39394de8b"
    else:
        checkpoint_id = "checkpoint_018bcfe9fbe0719ab771d281cd16bcd5"
        run_id = "run_018bcfe9fbe17211a807eaa7090f4105"
    return CheckpointIdentity(
        checkpoint_id=checkpoint_id,
        run_id=run_id,
        resolved_config_digest=Digest.of_text("distributed config").text,
        dataset_digest=Digest.of_text("deterministic affine samples").text,
        model_digest=Digest.of_text("reference-affine-v1").text,
        code_digest=Digest.of_text("source tree").text,
        toolchain_digest=Digest.of_text("pinned torch toolchain").text,
        topology_digest=Digest.of_text(topology).text,
    )


def optimizer(model: torch.nn.Module) -> torch.optim.AdamW:
    return torch.optim.AdamW(model.parameters(), lr=0.025, weight_decay=0.01, foreach=False)


def global_batch() -> SupervisedBatch:
    inputs = torch.tensor([[-2.0], [-0.5], [1.0], [2.0], [3.0]], dtype=torch.float32)
    return SupervisedBatch(inputs, (inputs * 3.0) - 1.0)


def make_distributed_trainer(
    context: DistributedContext,
    *,
    state: TrainingState | None = None,
    accumulation_steps: int = 1,
) -> tuple[torch.nn.parallel.DistributedDataParallel, torch.optim.AdamW, Trainer]:
    model = wrap_ddp(ReferenceAffine().to(context.device), context)
    optim = optimizer(model)
    trainer = Trainer(
        model,
        SupervisedMSETask(),
        optim,
        config=TrainerConfig(accumulation_steps=accumulation_steps),
        reducer=DDPReducer(context),
        state=state,
    )
    return model, optim, trainer


def assert_tree_equal(actual: object, expected: object) -> None:
    if isinstance(expected, torch.Tensor):
        assert isinstance(actual, torch.Tensor)
        torch.testing.assert_close(actual, expected, rtol=0.0, atol=0.0)
    elif isinstance(expected, dict):
        assert isinstance(actual, dict)
        assert actual.keys() == expected.keys()
        for key in expected:
            assert_tree_equal(actual[key], expected[key])
    elif isinstance(expected, (list, tuple)):
        assert isinstance(actual, type(expected))
        assert len(actual) == len(expected)
        for actual_item, expected_item in zip(actual, expected, strict=True):
            assert_tree_equal(actual_item, expected_item)
    else:
        assert actual == expected


def main() -> None:
    root = Path(os.environ["MINDCLADE_DDP_TEST_ROOT"])
    with distributed_session(DistributedConfig(backend="gloo", timeout_seconds=60)) as context:

        def trace(stage: str) -> None:
            print(f"rank={context.rank} stage={stage}", file=sys.stderr, flush=True)

        trace("initialized")
        global_item = global_batch()
        sharded = shard_supervised_batch(
            global_item,
            rank=context.rank,
            world_size=context.world_size,
        )
        local_item = sharded.batch

        # Unequal local sample counts still produce the exact global denominator
        # and the same update as a single-process full-batch reference.
        distributed_model, _, distributed_trainer = make_distributed_trainer(context)
        result = distributed_trainer.train((local_item,))[0]
        trace("unequal-batch")
        reference_model = ReferenceAffine()
        reference_optimizer = optimizer(reference_model)
        reference_trainer = Trainer(reference_model, SupervisedMSETask(), reference_optimizer)
        reference_trainer.train((global_item,))
        for actual, expected in zip(
            distributed_model.module.parameters(),
            reference_model.parameters(),
            strict=True,
        ):
            torch.testing.assert_close(actual, expected, rtol=1e-6, atol=1e-7)
        assert result.denominator == global_item.target_elements
        assert result.samples == global_item.batch_size
        assert result.microbatches == context.world_size

        # Multiple forwards retained for one optimizer group must cross DDP's
        # autograd boundary in one backward call. A forward-forward/backward-
        # backward sequence silently associates buckets with the wrong reducer
        # iteration and leaves ranks with different updates.
        accumulated_model, _, accumulated_trainer = make_distributed_trainer(
            context,
            accumulation_steps=2,
        )
        second_inputs = torch.tensor(
            [[-3.0], [-1.0], [0.25], [1.5], [4.0]],
            dtype=torch.float32,
        )
        second_global = SupervisedBatch(second_inputs, (second_inputs * -2.0) + 0.75)
        second_local = shard_supervised_batch(
            second_global,
            rank=context.rank,
            world_size=context.world_size,
            global_position=global_item.batch_size,
        ).batch
        accumulated_result = accumulated_trainer.train((local_item, second_local))[0]
        accumulated_reference = ReferenceAffine()
        accumulated_reference_optimizer = optimizer(accumulated_reference)
        Trainer(
            accumulated_reference,
            SupervisedMSETask(),
            accumulated_reference_optimizer,
            config=TrainerConfig(accumulation_steps=2),
        ).train((global_item, second_global))
        for actual, expected in zip(
            accumulated_model.module.parameters(),
            accumulated_reference.parameters(),
            strict=True,
        ):
            torch.testing.assert_close(actual, expected, rtol=1e-6, atol=1e-7)
        assert accumulated_result.denominator == (
            global_item.target_elements + second_global.target_elements
        )
        assert accumulated_result.samples == global_item.batch_size + second_global.batch_size
        trace("accumulation")

        # A cancellation requested by one rank is observed at the same safe point
        # by every rank, avoiding a peer entering the next collective alone.
        cancel_model, _, cancel_trainer = make_distributed_trainer(context)
        try:
            cancel_trainer.train(
                (local_item,),
                cancellation_check=lambda: context.rank == 1,
            )
        except Canceled:
            pass
        else:
            raise AssertionError("rank-local cancellation was not reduced globally")
        assert all(parameter.grad is None for parameter in cancel_model.parameters())
        trace("cancellation")

        # Schedule mismatch fails collectively before forward/backward.
        schedule_model, _, schedule_trainer = make_distributed_trainer(context)
        schedule = (local_item,) if context.rank == 0 else (local_item, local_item)
        try:
            schedule_trainer.train(schedule)
        except FailedPrecondition as error:
            assert "same microbatch" in str(error)
        else:
            raise AssertionError("mismatched rank schedules were accepted")
        assert all(parameter.grad is None for parameter in schedule_model.parameters())
        trace("schedule")

        # Same-world DCP restore must produce exact interrupted continuation.
        uninterrupted_model, uninterrupted_optimizer, uninterrupted_trainer = (
            make_distributed_trainer(context)
        )
        uninterrupted_trainer.train((local_item,) * 4)
        trace("uninterrupted")

        interrupted_model, interrupted_optimizer, interrupted_trainer = make_distributed_trainer(
            context
        )
        interrupted_trainer.train((local_item,) * 2)
        trace("interrupted")
        mismatched_identity = (
            identity("cpu-world-size-2") if context.rank == 0 else identity("cpu-world-size-1")
        )
        try:
            save_distributed_checkpoint(
                root / "mismatched-control",
                model=interrupted_model,
                optimizer=interrupted_optimizer,
                training_state=interrupted_trainer.state,
                identity=mismatched_identity,
                data_position=2 * global_item.batch_size,
            )
        except FailedPrecondition as error:
            assert "control identity" in str(error) or "another rank" in str(error)
        else:
            raise AssertionError("rank-divergent checkpoint identity was committed")
        assert not (root / "mismatched-control").exists()
        trace("control-mismatch")
        torch.manual_seed(10_000 + context.rank)
        expected_rng = torch.get_rng_state().clone()
        checkpoint = root / "world-size-2"
        save_distributed_checkpoint(
            checkpoint,
            model=interrupted_model,
            optimizer=interrupted_optimizer,
            training_state=interrupted_trainer.state,
            identity=identity("cpu-world-size-2"),
            data_position=2 * global_item.batch_size,
        )
        trace("saved")

        # A rank-local encode failure is reduced before any peer can enter the
        # replicated-byte collective. The following barrier is the bounded-
        # completion proof that the process group remains usable.
        original_encode = encode_state_component
        if context.rank == 1:

            def fail_encode(
                state: Mapping[str, object],
                *,
                component: str,
            ) -> tuple[bytes, bytes]:
                del state, component
                raise RuntimeError("injected rank-local DCP encode failure")

            dcp_module.__dict__["encode_state_component"] = fail_encode
        try:
            save_distributed_checkpoint(
                root / "encode-fault",
                model=interrupted_model,
                optimizer=interrupted_optimizer,
                training_state=interrupted_trainer.state,
                identity=identity("cpu-world-size-2"),
                data_position=2 * global_item.batch_size,
            )
        except (FailedPrecondition, RuntimeError):
            pass
        else:
            raise AssertionError("rank-local DCP encode failure was accepted")
        finally:
            dcp_module.__dict__["encode_state_component"] = original_encode
        assert not (root / "encode-fault").exists()
        torch.distributed.barrier()
        trace("encode-fault")

        # Writes occur only after encode/equality admission and close with a
        # status collective. Rank zero cleans staging while every peer waits in
        # the same cleanup-outcome collective, not a mismatched barrier.
        original_write = dcp_module._write_durable
        if context.rank == 1:

            def fail_write(path: Path, value: bytes) -> None:
                del value
                raise OSError(f"injected rank-local DCP write failure: {path.name}")

            dcp_module._write_durable = fail_write
        try:
            save_distributed_checkpoint(
                root / "write-fault",
                model=interrupted_model,
                optimizer=interrupted_optimizer,
                training_state=interrupted_trainer.state,
                identity=identity("cpu-world-size-2"),
                data_position=2 * global_item.batch_size,
            )
        except (FailedPrecondition, OSError):
            pass
        else:
            raise AssertionError("rank-local DCP write failure was accepted")
        finally:
            dcp_module._write_durable = original_write
        assert not (root / "write-fault").exists()
        assert not (root / ".write-fault.dcp-staging").exists()
        torch.distributed.barrier()
        trace("write-fault")

        # A write that returns success after changing bytes cannot be blessed by
        # hashing staging contents. Expected references come from the prepared
        # bytes, and every rank verifies its own RNG members before publication.
        if context.rank == 1:

            def tamper_write(path: Path, value: bytes) -> None:
                if path.name == "rank-00001.rng.json":
                    value = bytes((value[0] ^ 1,)) + value[1:]
                original_write(path, value)

            dcp_module._write_durable = tamper_write
        try:
            save_distributed_checkpoint(
                root / "write-tamper",
                model=interrupted_model,
                optimizer=interrupted_optimizer,
                training_state=interrupted_trainer.state,
                identity=identity("cpu-world-size-2"),
                data_position=2 * global_item.batch_size,
            )
        except (FailedPrecondition, InvalidArgument):
            pass
        else:
            raise AssertionError("silent rank-local DCP write tamper was accepted")
        finally:
            dcp_module._write_durable = original_write
        assert not (root / "write-tamper").exists()
        assert not (root / ".write-tamper.dcp-staging").exists()
        torch.distributed.barrier()
        trace("write-tamper")

        # A rank-local corrupt read is fully decoded/verified before the restore
        # admission collective. No peer reaches set_state_dict alone.
        corrupt_model, corrupt_optimizer, _ = make_distributed_trainer(context)
        original_read = read_checkpoint_member
        if context.rank == 1:

            def corrupt_read(root: Path, name: str, *, maximum_bytes: int) -> bytes:
                value = original_read(root, name, maximum_bytes=maximum_bytes)
                return value + b"corrupt" if name == "optimizer.json" else value

            dcp_module.__dict__["read_checkpoint_member"] = corrupt_read
        try:
            restore_distributed_checkpoint(
                checkpoint,
                model=corrupt_model,
                optimizer=corrupt_optimizer,
                expected_identity=identity("cpu-world-size-2"),
            )
        except (FailedPrecondition, InvalidArgument):
            pass
        else:
            raise AssertionError("rank-local corrupt DCP member was accepted")
        finally:
            dcp_module.__dict__["read_checkpoint_member"] = original_read
        assert not corrupt_optimizer.state
        torch.distributed.barrier()
        trace("corrupt-read")

        divergent_restore_model, divergent_restore_optimizer, _ = make_distributed_trainer(context)
        divergent_expected_identity = (
            identity("cpu-world-size-2") if context.rank == 0 else identity("cpu-world-size-1")
        )
        try:
            restore_distributed_checkpoint(
                checkpoint,
                model=divergent_restore_model,
                optimizer=divergent_restore_optimizer,
                expected_identity=divergent_expected_identity,
            )
        except FailedPrecondition as error:
            assert "identity" in str(error) or "another rank" in str(error)
        else:
            raise AssertionError("rank-divergent restore identity was accepted")
        assert not divergent_restore_optimizer.state
        trace("restore-control-mismatch")
        torch.manual_seed(1)
        resumed_model, resumed_optimizer, _ = make_distributed_trainer(context)
        restored = restore_distributed_checkpoint(
            checkpoint,
            model=resumed_model,
            optimizer=resumed_optimizer,
            expected_identity=identity("cpu-world-size-2"),
        )
        trace("restored")
        assert restored.exact_resume and restored.source_rank == context.rank
        torch.testing.assert_close(torch.get_rng_state(), expected_rng, rtol=0.0, atol=0.0)
        resumed_trainer = Trainer(
            resumed_model,
            SupervisedMSETask(),
            resumed_optimizer,
            reducer=DDPReducer(context),
            state=restored.training_state,
        )
        resumed_trainer.train((local_item,) * 2)
        assert resumed_trainer.state == uninterrupted_trainer.state
        assert_tree_equal(
            resumed_model.module.state_dict(), uninterrupted_model.module.state_dict()
        )
        assert_tree_equal(resumed_optimizer.state_dict(), uninterrupted_optimizer.state_dict())
        trace("exact-resume")

        # A world-size-one replicated checkpoint can seed world-size two, but RNG
        # is intentionally not claimed as an exact continuation.
        portable_model, portable_optimizer, _ = make_distributed_trainer(context)
        portable = restore_distributed_checkpoint(
            root / "world-size-1",
            model=portable_model,
            optimizer=portable_optimizer,
            expected_identity=identity("cpu-world-size-1"),
            allow_replicated_world_size_change=True,
        )
        trace("portable-restored")
        assert not portable.exact_resume and portable.source_rank == 0
        assert portable.training_state.optimizer_steps == 1
        Trainer(
            portable_model,
            SupervisedMSETask(),
            portable_optimizer,
            reducer=DDPReducer(context),
            state=portable.training_state,
        ).train((local_item,))
        trace("portable-trained")

        torch.distributed.barrier()
        if context.rank == 0:
            (root / "success.json").write_text(
                json.dumps({"world_size": context.world_size, "backend": context.backend}),
                encoding="utf-8",
            )


if __name__ == "__main__":
    main()
