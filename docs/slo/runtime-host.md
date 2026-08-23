# Runtime host SLO

**Status:** no approved objective. No instrumentation exists to measure one.

Objectives are defined before production promotion. `services/runtime_host` assembles its readiness
correctly — `set_process_supervisor_ready(true)` and `set_gpu_ready(true)` are asserted during
construction (`services/runtime_host/src/server.rs:81-82`) and `resume_admission` sets `accepting`
(`:193-194`) — so unlike the gateway it can genuinely report ready. What it cannot do is report
anything else: the service emits no metrics, no structured logs, and no traces. Every indicator for
it must be derived by the calling gateway or by the surrounding infrastructure, and until that
derivation exists and is measured in staging, any numeric target would be a guess.

## Unratified candidate — not an agreed target

A previous revision recorded `99.9%` availability "for admitted production traffic where
applicable". The identical sentence appeared in four other unrelated SLO documents with no owner
record, no measurement window, and no staging evidence behind it. It is retained here as an
**unratified candidate** so the earlier choice is neither carried forward as agreed nor silently
erased. Ratification requires representative staging load, a named owner, and an objective stated
separately for caller errors, model-execution errors, and host availability errors.

## Bounds already enforced

These are real and can be asserted today. They constrain any future objective; they are not
themselves objectives.

| Bound | Value | Source |
| --- | --- | --- |
| `MAX_CONTROL_FRAME_BYTES` | 1 MiB | `src/grpc.rs:29` |
| `STATUS_QUEUE_CAPACITY` | 32 | `src/grpc.rs:30` |
| `GRPC_DRAIN_TIMEOUT` | 30 s | `src/grpc.rs:31` |
| `FIRST_COMMAND_TIMEOUT` | 30 s | `src/grpc.rs:32` |
| `CANCELLATION_GRACE` | 5 s | `src/grpc.rs:33` |
| `MAX_FRAME_BYTES` (control IPC) | 1 MiB | `src/async_ipc.rs:24` |
| `MAX_CONTROL_CONNECTIONS` | 128 | `src/async_ipc.rs:25` |
| `CONTROL_IO_TIMEOUT` | 30 s | `src/async_ipc.rs:26` |
| `CONTROL_HANDLER_TIMEOUT` | 5 min | `src/async_ipc.rs:27` |
| `CONNECTION_DRAIN_TIMEOUT` | 30 s | `src/async_ipc.rs:28` |

`CONTROL_HANDLER_TIMEOUT` at five minutes is an order of magnitude above every other budget here.
It is a bound, so it is not unbounded, but a latency objective must state explicitly whether a
control handler running to that ceiling counts against the error budget. That decision belongs to
the owner, not to this document.

## Correctness invariants (release-blocking, not traded for availability)

Python worker termination is confirmed before GPU or model-slot reservations are released, fencing
tokens are never reused, and bounded queues are never converted into unbounded buffers. Availability
is never restored by skipping worker fencing. Bounded admission, cancellation, and shutdown budgets
must be release-qualified before production promotion; they are not release-qualified today. SLO
exclusions require an incident or evidence record, not an ad hoc dashboard annotation.
