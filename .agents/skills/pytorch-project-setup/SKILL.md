---
name: pytorch-project-setup
description: Scaffold or modernize a PyTorch repository with a compatible environment, project layout, configuration, smoke tests, and developer commands. Use when starting a PyTorch project, repairing packaging or dependency setup, adding CI-ready tooling, or making a repository reproducible. Do not use for model architecture, training-loop, or performance work by itself.
license: MIT
compatibility: Designed for Codex and other Agent Skills-compatible clients. Project commands require Python and the repository's installed PyTorch version.
metadata:
  version: "1.0.0"
  domain: "pytorch"
---
# Objective

Create the smallest maintainable PyTorch project setup that fits the repository, hardware target, and requested workflow. Preserve established conventions unless there is evidence they are broken.

# Workflow

1. Read all applicable `AGENTS.md` files and repository documentation before editing.
2. Inventory the existing environment and packaging files. Identify the active package manager, Python constraint, PyTorch pin, accelerator expectations, test runner, formatter, linter, and CI commands.
3. Run `scripts/project_probe.py --root .` when a compact environment and repository report would help. Do not treat the probe as authoritative when a container or remote runner differs from the target environment.
4. Decide whether the task is a new scaffold or a minimal repair. For a repair, change only the broken layer and retain the current package manager.
5. Establish a clear source, test, configuration, and entry-point layout. Follow the existing import style; do not force a `src/` migration into a mature project without a reason.
6. Add dependency declarations without guessing a CUDA, ROCm, XPU, MPS, or CPU wheel. Respect existing lockfiles and installation channels. Do not upgrade PyTorch unless the user requested it or the current constraint is demonstrably incompatible.
7. Add one fast CPU smoke path that imports the package, creates a tiny module or tensor operation, and runs without network access or downloaded data.
8. Add focused developer commands for tests and static checks. Prefer the repository's current tools over introducing redundant alternatives.
9. Run the relevant commands from a clean working directory. Confirm imports work from the same context CI or users will use.
10. Report the files changed, exact commands run, detected PyTorch and Python versions, assumptions about hardware, and anything intentionally left unpinned.

# Non-negotiable rules

- Never mix package managers casually, such as adding `requirements.txt` to a Poetry or uv project without a compatibility reason.
- Never copy a platform-specific PyTorch install command from memory. Use repository constraints or the official installation selector for the target platform.
- Keep optional accelerator and development dependencies separated when the project supports multiple deployment targets.
- Avoid import-time device initialization, dataset downloads, distributed initialization, or expensive model construction.
- Do not put secrets, local absolute paths, model weights, generated datasets, profiler traces, or checkpoints in version control.
- Make the default smoke test CPU-capable even when the production target is an accelerator, unless the repository is explicitly accelerator-only.

# Definition of done

- A fresh environment can install the project using the documented workflow.
- The package imports without path hacks.
- A fast smoke test passes without external data.
- Existing tests still pass or any pre-existing failures are clearly separated from the changes.
- The setup documents hardware-specific installation decisions rather than silently encoding guesses.

Read [the project layout reference](references/project-layout.md) for decision guidance and example structures.
