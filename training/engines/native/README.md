# Native training engine

**Status:** thin CPU eager reference adapter implemented; launcher, precision,
parallelism, accelerator, and distributed modules remain scaffolded.

`NativeEngine` requires the authoritative `training.core.Trainer` and delegates
training and evaluation directly. It owns no second loop, device selection,
provider initialization, networking, telemetry, checkpointing, or worker
lifecycle. This separation keeps numerical ordering in one implementation while
leaving deployable composition under the services boundary.
