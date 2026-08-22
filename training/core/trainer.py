# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Deterministic, single-owner eager float32 reference trainer."""

from __future__ import annotations

import math
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from typing import Final, Protocol

import torch
from torch import nn
from torch.nn.parallel import DistributedDataParallel

from libs.python.errors import Canceled, FailedPrecondition, InvalidArgument, ResourceExhausted
from training.contracts import StepResult, SupervisedBatch, Task, TaskResult, TrainingState

from .reduction import LocalReducer, Reducer

MAXIMUM_ACCUMULATION_STEPS: Final = 1_024
MAXIMUM_MICROBATCHES_PER_CALL: Final = 100_000
MAXIMUM_GRADIENT_CLIP_NORM: Final = 1_000_000.0
MAXIMUM_REDUCTION_DENOMINATOR: Final = (1 << 63) - 1

CancellationCheck = Callable[[], bool]


class Scheduler(Protocol):
    def step(self) -> object: ...


@dataclass(frozen=True, slots=True)
class TrainerConfig:
    """Bounded behavior for an eager float32 training loop."""

    accumulation_steps: int = 1
    maximum_microbatches_per_call: int = 10_000
    gradient_clip_norm: float | None = None

    def __post_init__(self) -> None:
        _bounded_integer(
            self.accumulation_steps,
            name="accumulation_steps",
            maximum=MAXIMUM_ACCUMULATION_STEPS,
        )
        _bounded_integer(
            self.maximum_microbatches_per_call,
            name="maximum_microbatches_per_call",
            maximum=MAXIMUM_MICROBATCHES_PER_CALL,
        )
        if self.gradient_clip_norm is not None:
            value = self.gradient_clip_norm
            if (
                isinstance(value, bool)
                or not isinstance(value, int | float)
                or not math.isfinite(value)
                or not 0.0 < float(value) <= MAXIMUM_GRADIENT_CLIP_NORM
            ):
                raise InvalidArgument(
                    "gradient_clip_norm is outside bounds",
                    reason="training_gradient_clip",
                )


class Trainer:
    """The authoritative optimizer lifecycle for the eager reference path.

    One instance has one owner and is intentionally not thread-safe. Progress is
    measured in committed optimizer steps. A group performs, in order:

    ``zero_grad -> forwards -> normalized backwards -> finite-gradient check ->
    optional clipping -> optimizer step -> optional scheduler step``.
    """

    def __init__(
        self,
        model: nn.Module,
        task: Task,
        optimizer: torch.optim.Optimizer,
        *,
        config: TrainerConfig | None = None,
        scheduler: Scheduler | None = None,
        state: TrainingState | None = None,
        reducer: Reducer | None = None,
    ) -> None:
        if not isinstance(model, nn.Module):
            raise InvalidArgument(
                "trainer model must be an nn.Module",
                reason="training_model",
            )
        if not callable(getattr(task, "compute", None)):
            raise InvalidArgument(
                "trainer task must implement compute",
                reason="training_task",
            )
        if not isinstance(optimizer, torch.optim.Optimizer):
            raise InvalidArgument(
                "trainer optimizer must be torch.optim.Optimizer",
                reason="training_optimizer",
            )
        resolved_config = config or TrainerConfig()
        if not isinstance(resolved_config, TrainerConfig):
            raise InvalidArgument(
                "trainer config must be TrainerConfig",
                reason="training_config",
            )
        if scheduler is not None and not callable(getattr(scheduler, "step", None)):
            raise InvalidArgument(
                "trainer scheduler must implement step",
                reason="training_scheduler",
            )
        resolved_state = state or TrainingState()
        if not isinstance(resolved_state, TrainingState):
            raise InvalidArgument(
                "trainer state must be TrainingState",
                reason="training_state",
            )
        resolved_reducer = reducer or LocalReducer()
        if not isinstance(resolved_reducer, Reducer):
            raise InvalidArgument(
                "trainer reducer must implement the reduction contract",
                reason="training_reducer",
            )
        if (
            isinstance(resolved_reducer.world_size, bool)
            or not isinstance(resolved_reducer.world_size, int)
            or resolved_reducer.world_size <= 0
            or not math.isfinite(resolved_reducer.backward_scale)
            or resolved_reducer.backward_scale <= 0.0
        ):
            raise InvalidArgument(
                "trainer reducer properties are invalid",
                reason="training_reducer",
            )
        is_ddp = isinstance(model, DistributedDataParallel)
        if (resolved_reducer.world_size > 1) != is_ddp:
            raise InvalidArgument(
                "multi-rank reducers and DistributedDataParallel models must be used together",
                reason="training_reducer_model",
            )

        self._model = model
        self._task = task
        self._optimizer = optimizer
        self._config = resolved_config
        self._scheduler = scheduler
        self._state = resolved_state
        self._reducer = resolved_reducer
        self._failure_reason: str | None = None
        self._optimizer_parameters = _validate_optimizer_ownership(model, optimizer)
        self._device = _validate_float32_model(model)

    @property
    def model(self) -> nn.Module:
        return self._model

    @property
    def state(self) -> TrainingState:
        return self._state

    @property
    def optimizer(self) -> torch.optim.Optimizer:
        return self._optimizer

    @property
    def scheduler(self) -> Scheduler | None:
        return self._scheduler

    @property
    def config(self) -> TrainerConfig:
        return self._config

    @property
    def reducer(self) -> Reducer:
        return self._reducer

    @property
    def healthy(self) -> bool:
        """Whether no possibly-partial optimizer transaction has failed."""

        return self._failure_reason is None

    @property
    def failure_reason(self) -> str | None:
        return self._failure_reason

    def train(
        self,
        batches: Sequence[SupervisedBatch],
        *,
        cancellation_check: CancellationCheck | None = None,
    ) -> tuple[StepResult, ...]:
        """Train on a finite sequence and return one result per optimizer step."""

        self._ensure_healthy()
        resolved_batches = _validate_batches(batches, self._config.maximum_microbatches_per_call)
        _validate_cancellation_check(cancellation_check)
        device = _validate_float32_model(self._model)
        if device != self._device:
            raise FailedPrecondition(
                "trainer model placement changed after construction",
                reason="training_model_device",
            )
        _validate_batch_devices(resolved_batches, device)
        self._reducer.validate_schedule(
            microbatches=len(resolved_batches),
            accumulation_steps=self._config.accumulation_steps,
            device=device,
        )
        _validate_finite_parameters(self._optimizer_parameters)

        previous_mode = self._model.training
        results: list[StepResult] = []
        self._model.train(True)
        try:
            for start in range(0, len(resolved_batches), self._config.accumulation_steps):
                group = resolved_batches[start : start + self._config.accumulation_steps]
                results.append(self._train_group(group, cancellation_check, device))
            return tuple(results)
        finally:
            self._model.train(previous_mode)

    def evaluate(
        self,
        batches: Sequence[SupervisedBatch],
        *,
        cancellation_check: CancellationCheck | None = None,
    ) -> StepResult:
        """Evaluate without changing model mode, parameters, gradients, or state."""

        self._ensure_healthy()
        resolved_batches = _validate_batches(batches, self._config.maximum_microbatches_per_call)
        _validate_cancellation_check(cancellation_check)
        device = _validate_float32_model(self._model)
        if device != self._device:
            raise FailedPrecondition(
                "trainer model placement changed after construction",
                reason="training_model_device",
            )
        _validate_batch_devices(resolved_batches, device)
        self._reducer.validate_schedule(
            microbatches=len(resolved_batches),
            accumulation_steps=1,
            device=device,
        )
        _validate_finite_parameters(self._optimizer_parameters)

        previous_mode = self._model.training
        total_loss_sum = 0.0
        denominator = 0
        samples = 0
        self._model.eval()
        try:
            with torch.inference_mode():
                for batch in resolved_batches:
                    self._check_cancellation(
                        cancellation_check,
                        operation="evaluate_forward",
                        device=device,
                    )
                    task_result = self._compute_task(batch)
                    self._check_cancellation(
                        cancellation_check,
                        operation="evaluate_forward",
                        device=device,
                    )
                    total_loss_sum += float(task_result.loss_sum.detach().item())
                    denominator = _add_denominator(denominator, task_result.denominator)
                    samples += batch.batch_size
            reduced = self._reducer.reduce_counts(
                denominator=denominator,
                microbatches=len(resolved_batches),
                samples=samples,
                device=device,
            )
            total_loss_sum = self._reducer.reduce_loss_sum(total_loss_sum, device=device)
            self._check_cancellation(
                cancellation_check,
                operation="evaluate_complete",
                device=device,
            )
        finally:
            self._model.train(previous_mode)

        mean_loss = total_loss_sum / reduced.denominator
        return StepResult(
            state=self._state,
            loss_sum=total_loss_sum,
            denominator=reduced.denominator,
            microbatches=reduced.microbatches,
            samples=reduced.samples,
            optimizer_step=False,
            metrics={"loss": mean_loss},
        )

    def _train_group(
        self,
        group: tuple[SupervisedBatch, ...],
        cancellation_check: CancellationCheck | None,
        device: torch.device,
    ) -> StepResult:
        self._check_cancellation(
            cancellation_check,
            operation="optimizer_group",
            device=device,
        )
        self._optimizer.zero_grad(set_to_none=True)
        optimizer_attempted = False
        try:
            task_results: list[TaskResult] = []
            denominator = 0
            samples = 0
            for batch in group:
                self._check_cancellation(
                    cancellation_check,
                    operation="train_forward",
                    device=device,
                )
                task_result = self._compute_task(batch)
                self._check_cancellation(
                    cancellation_check,
                    operation="train_forward",
                    device=device,
                )
                task_results.append(task_result)
                denominator = _add_denominator(denominator, task_result.denominator)
                samples += batch.batch_size

            reduced = self._reducer.reduce_counts(
                denominator=denominator,
                microbatches=len(group),
                samples=samples,
                device=device,
            )

            self._check_cancellation(
                cancellation_check,
                operation="train_backward",
                device=device,
            )
            if any(not task_result.loss_sum.requires_grad for task_result in task_results):
                raise FailedPrecondition(
                    "training loss must require gradients",
                    reason="training_loss_autograd",
                )
            # DDP requires the forward/backward sequence for one reducer iteration to stay
            # paired. Running every forward first and then calling backward separately for
            # each retained graph silently associates reducer buckets with the wrong forward
            # when accumulation_steps > 1. Build one scalar objective for the whole optimizer
            # group and cross the autograd/DDP boundary exactly once.
            accumulated_loss = torch.stack(
                tuple(task_result.loss_sum for task_result in task_results)
            ).sum()
            normalized_loss = accumulated_loss * self._reducer.backward_scale / reduced.denominator
            torch.autograd.backward(normalized_loss)
            total_loss_sum = sum(
                float(task_result.loss_sum.detach().item()) for task_result in task_results
            )
            self._check_cancellation(
                cancellation_check,
                operation="train_backward",
                device=device,
            )

            total_loss_sum = self._reducer.reduce_loss_sum(total_loss_sum, device=device)

            # Validate the prospective counters before crossing the non-transactional
            # optimizer boundary. The immutable value is published only after the optimizer
            # and scheduler both succeed.
            next_state = self._state.after_optimizer_step(
                microbatches=reduced.microbatches,
                samples=reduced.samples,
            )
            _validate_finite_gradients(self._optimizer_parameters)
            gradient_norm = self._clip_gradients()
            self._check_cancellation(
                cancellation_check,
                operation="optimizer_step",
                device=device,
            )
            # Optimizer implementations are not transactional. From this point onward any
            # exception may have left parameters or optimizer/scheduler state partially
            # mutated, so these live objects cannot be retried safely.
            optimizer_attempted = True
            self._optimizer.step()
            _validate_finite_parameters(self._optimizer_parameters)
            if self._scheduler is not None:
                self._scheduler.step()
            self._optimizer.zero_grad(set_to_none=True)

            self._state = next_state
            metrics = {"loss": total_loss_sum / reduced.denominator}
            if gradient_norm is not None:
                metrics["gradient_norm"] = gradient_norm
            return StepResult(
                state=self._state,
                loss_sum=total_loss_sum,
                denominator=reduced.denominator,
                microbatches=reduced.microbatches,
                samples=reduced.samples,
                optimizer_step=True,
                metrics=metrics,
            )
        except BaseException as error:
            if optimizer_attempted:
                self._failure_reason = (
                    f"optimizer transaction failed after mutation began: {type(error).__name__}"
                )
            self._optimizer.zero_grad(set_to_none=True)
            raise

    def _compute_task(self, batch: SupervisedBatch) -> TaskResult:
        result = self._task.compute(self._model, batch)
        if not isinstance(result, TaskResult):
            raise FailedPrecondition(
                "training task returned an invalid result type",
                reason="training_task_result",
            )
        if result.loss_sum.device != self._device:
            raise FailedPrecondition(
                "training task loss must remain on the trainer device",
                reason="training_task_result_device",
            )
        return result

    def _check_cancellation(
        self,
        cancellation_check: CancellationCheck | None,
        *,
        operation: str,
        device: torch.device,
    ) -> None:
        requested = cancellation_check is not None and cancellation_check()
        if not isinstance(requested, bool):
            raise InvalidArgument(
                "cancellation_check must return a boolean",
                reason="training_cancellation_result",
            )
        if self._reducer.any_true(requested, device=device):
            raise Canceled(
                "training operation was canceled at a safe point",
                reason="training_canceled",
                operation=operation,
            )

    def _clip_gradients(self) -> float | None:
        maximum = self._config.gradient_clip_norm
        if maximum is None:
            return None
        try:
            norm = torch.nn.utils.clip_grad_norm_(
                self._optimizer_parameters,
                max_norm=float(maximum),
                error_if_nonfinite=True,
                foreach=False,
            )
        except RuntimeError as error:
            raise FloatingPointError("gradient norm is not finite") from error
        value = float(norm.detach().item() if isinstance(norm, torch.Tensor) else norm)
        if not math.isfinite(value):
            raise FloatingPointError("gradient norm is not finite")
        return value

    def _ensure_healthy(self) -> None:
        if self._failure_reason is not None:
            raise FailedPrecondition(
                "trainer is poisoned by a possibly partial optimizer transaction; restore a "
                "verified checkpoint into fresh objects before continuing",
                reason="training_optimizer_transaction_partial",
                fields={"failure": self._failure_reason},
            )


def _validate_batches(batches: object, maximum: int) -> tuple[SupervisedBatch, ...]:
    if isinstance(batches, (str, bytes)) or not isinstance(batches, Sequence):
        raise InvalidArgument(
            "trainer batches must be a finite sequence",
            reason="training_batches",
        )
    count = len(batches)
    if count == 0:
        raise InvalidArgument(
            "trainer requires at least one batch",
            reason="training_batches_empty",
        )
    if count > maximum:
        raise ResourceExhausted(
            "trainer batch count exceeds the configured bound",
            reason="training_batches_bound",
        )
    resolved = tuple(batches)
    if len(resolved) != count or any(not isinstance(batch, SupervisedBatch) for batch in resolved):
        raise InvalidArgument(
            "trainer batches must contain only SupervisedBatch values",
            reason="training_batch_type",
        )
    return resolved


def _validate_optimizer_ownership(
    model: nn.Module, optimizer: torch.optim.Optimizer
) -> tuple[nn.Parameter, ...]:
    model_parameters = {
        id(parameter): parameter for parameter in model.parameters() if parameter.requires_grad
    }
    optimized: list[nn.Parameter] = []
    identities: set[int] = set()
    for group in optimizer.param_groups:
        for parameter in group.get("params", ()):
            if not isinstance(parameter, nn.Parameter):
                raise InvalidArgument(
                    "optimizer groups must contain nn.Parameter values",
                    reason="training_optimizer_parameter_type",
                )
            identity = id(parameter)
            if identity in identities:
                raise InvalidArgument(
                    "optimizer contains a duplicate parameter",
                    reason="training_optimizer_parameter_duplicate",
                )
            if identity not in model_parameters:
                raise InvalidArgument(
                    "optimizer parameter does not belong to the trainer model",
                    reason="training_optimizer_parameter_owner",
                )
            if not parameter.requires_grad:
                raise InvalidArgument(
                    "optimizer contains a frozen parameter",
                    reason="training_optimizer_parameter_frozen",
                )
            identities.add(identity)
            optimized.append(parameter)
    if not optimized:
        raise InvalidArgument(
            "trainer optimizer has no parameters",
            reason="training_optimizer_parameter_empty",
        )
    missing = set(model_parameters) - identities
    if missing:
        raise InvalidArgument(
            "trainer optimizer must own every trainable model parameter",
            reason="training_optimizer_parameter_missing",
            fields={"missing_count": str(len(missing))},
        )
    return tuple(optimized)


def _validate_float32_model(model: nn.Module) -> torch.device:
    tensors = (*model.named_parameters(), *model.named_buffers())
    device: torch.device | None = None
    for name, tensor in tensors:
        if tensor.device.type not in {"cpu", "cuda"}:
            raise FailedPrecondition(
                "reference trainer supports CPU or CUDA model state only",
                reason="training_model_device",
                fields={"tensor": name},
            )
        if device is None:
            device = tensor.device
        elif tensor.device != device:
            raise FailedPrecondition(
                "reference trainer requires model state on one device",
                reason="training_model_device",
                fields={"tensor": name},
            )
        if tensor.is_floating_point() and tensor.dtype is not torch.float32:
            raise FailedPrecondition(
                "reference trainer supports float32 model state only",
                reason="training_model_dtype",
                fields={"tensor": name},
            )
    if device is None:
        raise FailedPrecondition(
            "reference trainer model must contain parameter or buffer state",
            reason="training_model_state_empty",
        )
    return device


def _validate_batch_devices(
    batches: tuple[SupervisedBatch, ...],
    device: torch.device,
) -> None:
    for index, batch in enumerate(batches):
        if batch.device != device:
            raise FailedPrecondition(
                "trainer batches and model must be on the same device",
                reason="training_batch_device",
                fields={"batch": str(index)},
            )


def _validate_finite_parameters(parameters: tuple[nn.Parameter, ...]) -> None:
    for parameter in parameters:
        if not bool(torch.isfinite(parameter.detach()).all().item()):
            raise FloatingPointError("model parameter is not finite")


def _validate_finite_gradients(parameters: tuple[nn.Parameter, ...]) -> None:
    for parameter in parameters:
        gradient = parameter.grad
        if gradient is None:
            raise FailedPrecondition(
                "every optimized parameter must receive a gradient",
                reason="training_gradient_missing",
            )
        if gradient.is_sparse:
            raise FailedPrecondition(
                "reference trainer does not support sparse gradients",
                reason="training_gradient_sparse",
            )
        if gradient.device != parameter.device or gradient.dtype is not torch.float32:
            raise FailedPrecondition(
                "reference trainer gradients must be float32 on the parameter device",
                reason="training_gradient_placement",
            )
        if not bool(torch.isfinite(gradient.detach()).all().item()):
            raise FloatingPointError("model gradient is not finite")


def _add_denominator(current: int, increment: int) -> int:
    value = current + increment
    if value > MAXIMUM_REDUCTION_DENOMINATOR:
        raise ResourceExhausted(
            "training loss denominator exceeds bounds",
            reason="training_denominator_bound",
        )
    return value


def _bounded_integer(value: object, *, name: str, maximum: int) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= maximum:
        raise InvalidArgument(
            f"trainer {name} is outside bounds",
            reason="training_config_value",
            fields={"field": name},
        )
    return value


def _validate_cancellation_check(value: CancellationCheck | None) -> None:
    if value is not None and not callable(value):
        raise InvalidArgument(
            "cancellation_check must be callable",
            reason="training_cancellation_check",
        )
