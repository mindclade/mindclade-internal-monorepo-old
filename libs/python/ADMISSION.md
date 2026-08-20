# Python library admission

A reserved `libs/python` package becomes implemented only when one concrete in-tree consumer
needs a reusable mechanism and the following evidence lands in the same change:

- a named owner and component entry with an explicit public contract;
- package boundaries and non-responsibilities that keep product policy and providers out;
- validated inputs, bounded collections and I/O, deterministic behavior, and explicit failure
  and retry semantics;
- cancellation and cleanup behavior for blocking, asynchronous, process, or device work;
- package-local success, edge, failure, security, and numerical tests as applicable;
- a real Bazel `py_library` plus non-vacuous `pytest_test` targets and `py.typed` data;
- strict mypy and Ruff coverage with no broad ignores;
- a representative benchmark before any performance-specific optimization;
- compatibility and cross-language golden evidence for any serialized or wire-visible value;
- documentation of resource limits, trust boundaries, and operational assumptions.

HTTP routes, CLI composition, credentials, provider clients, deployment health/drain wiring,
and release orchestration are not admitted here. They belong at service or tool composition
roots. A generic abstraction also requires at least two real consumers or an accepted ADR.

Scaffold files and skipped scaffold tests are reservations, not partially implemented APIs.
When a package is admitted, remove every `SCAFFOLD_PATH` and `pytest.mark.scaffold` in that
package and lower the repository scaffold ratchet in the same commit.
