# Project setup reference

## First inspect

Look for these files before proposing a layout or dependency change:

- `AGENTS.md`, `README*`, `CONTRIBUTING*`
- `pyproject.toml`, `setup.py`, `setup.cfg`
- `uv.lock`, `poetry.lock`, `pdm.lock`, `Pipfile.lock`
- `requirements*.txt`, `environment*.yml`, `conda-lock*`
- `pytest.ini`, `tox.ini`, `noxfile.py`
- `.pre-commit-config.yaml`, linter and type-checker configuration
- Dockerfiles, dev-container files, CI workflows, job launch scripts

## Layout decision

For a new reusable package, a conventional structure is:

```text
project/
  pyproject.toml
  README.md
  src/package_name/
    __init__.py
    models.py
    data.py
    train.py
  tests/
    test_smoke.py
  configs/
```

For an existing application, keep the current package boundary if imports and tests are already coherent. A migration to `src/` layout can prevent accidental imports from the working tree, but it is not automatically worth a broad diff.

## Dependency policy

1. Preserve the active package manager and lockfile.
2. Keep the repository's PyTorch version constraint unless the task requires a supported API that is unavailable.
3. Treat accelerator builds as environment-specific. CUDA, ROCm, XPU, and CPU installation sources can differ by operating system and driver stack.
4. Keep optional packages optional. Do not make ONNX Runtime, TensorBoard, distributed launchers, or visualization libraries mandatory unless the core workflow needs them.
5. Record enough version information to reproduce a run, while avoiding pins that prevent supported security or patch updates without reason.

## Minimal smoke coverage

A useful smoke test should:

- import the installed package;
- construct a tiny model or tensor function;
- run a CPU forward pass;
- assert output shape and finite values;
- finish in seconds;
- avoid internet access and large fixtures.

## Environment report

Record at least:

- Python version and implementation;
- PyTorch version and build string;
- operating system and architecture;
- accelerator availability relevant to the target;
- package manager and lockfile;
- exact test command.

Official references:

- PyTorch installation: https://pytorch.org/get-started/locally/
- PyTorch documentation: https://docs.pytorch.org/docs/stable/index.html
- Codex skills: https://developers.openai.com/codex/build-skills
