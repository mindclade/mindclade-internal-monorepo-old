<p align="center">
  <a href="../README.md"><img src="../.github/assets/brand/mindclade-wordmark.png" alt="Mindclade" width="240"></a>
</p>

[← Repository home](../README.md) · [Qualification](../QUALIFICATION.md) · [Validation](../VALIDATION.md)

# Cross-system tests

> **Maturity:** Mixed; evidence is scoped to the suite, environment, and
> component under test.
> **Primary implementation:** Cross-package qualification and golden fixtures.

`tests/` holds repository-level tests that cross package or language boundaries.
Unit tests remain beside their owning packages.

## What's here

| Path | Responsibility |
| --- | --- |
| [`integration/`](integration/) | Package, provider, vertical-slice, and cross-language integration |
| [`e2e/`](e2e/) | Target-state end-to-end workflows and release-candidate scenarios |
| [`numerical/`](numerical/) | Numerical parity, determinism, precision, and resume behavior |
| [`performance/`](performance/) | Hardware-scoped throughput, latency, and bandwidth baselines |
| [`resilience/`](resilience/) | Failure, retry, outage, and recovery behavior |
| [`security/`](security/) | Cross-system security and isolation checks |
| [`scale/`](scale/) | Capacity and decomposition-threshold evidence |
| [`goldens/`](goldens/) | Reviewed stable expected outputs |
| [`fixtures/`](fixtures/) | Shared test inputs for cross-package suites |

## Boundary

- A test file can describe target-state behavior without proving the behavior is
  implemented; inspect its assertions and execution lane.
- Performance results are valid only for their recorded hardware, toolchain,
  configuration, and dataset envelope.
- Hidden evaluation and sensitive fixtures must remain isolated from training
  and public artifacts.
- Promotion evidence belongs in the relevant qualification record, not in an
  untracked local test result.

## Start here

- Use [`QUALIFICATION.md`](../QUALIFICATION.md) for recorded evidence.
- Use [`VALIDATION.md`](../VALIDATION.md) for exact local and connected lanes.
- Read a suite's local README, such as
  [`performance/README.md`](performance/README.md), before interpreting results.
