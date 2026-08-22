# ADR-0025: Exact training-service composition layer

- **Status:** Accepted
- **Date:** 2026-08-21
- **Scope:** `services/workers/training` dependency composition

## Context

ADR-0010 makes services deployable composition roots while reusable numerical
behavior remains in its owning domain. The repository-wide services layer may
consume offline and runtime code but deliberately cannot consume `training/`.
The concrete training worker must assemble the authoritative trainer rather
than copy its behavior or hide the dependency behind a dynamic import. Opening
services-to-training for every service would weaken a boundary for one exact
consumer.

## Decision

Classify `//services/workers/training/...` in a dedicated `training_service`
Bazel layer. It may depend on foundation, offline/model, training, build/test/
release support, and itself. It may not depend on other deployables or widen
the general services layer.

The general services package group includes `//services/...` and explicitly
excludes `//services/workers/training/...`. The layer checker implements the
same positive and negative package-pattern semantics and continues to reject
unclassified and multiply classified packages.

## Consequences

- The training worker is a transparent Bazel composition root over one
  authoritative training implementation.
- Other services remain unable to import training code.
- Training, libraries, and model code remain unable to import the worker.
- A second service requiring this direction needs a new reviewed policy change;
  this decision is not a wildcard exception.

## Enforcement

- `tools/build/bazel/layers.bzl` owns the exact package-group carve-out and
  fail-closed allow matrix.
- `tools/analysis/check_bazel_layers.py` and its tests enforce identical
  inclusion/exclusion and dependency behavior.
- No temporary layer exception is created for this permanent direction.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does
not widen the layer.
