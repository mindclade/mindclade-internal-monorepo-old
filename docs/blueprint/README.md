# Production blueprint materialization

This directory contains the approved production blueprint and the exact list of
explicit repository paths represented by the scaffold.

- `production-monorepo-blueprint.md` describes architecture, ownership,
  pipelines, language boundaries, qualification, and adoption.
- `production-monorepo-paths.txt` is the machine-readable inventory of every
  explicit blueprint file.

Run:

```bash
python tools/analysis/check_blueprint_scaffold.py
```

The check fails when a blueprint file is missing, duplicated in the manifest,
uses an unsafe path, or is unexpectedly empty. Lockfiles that require connected
dependency resolution may remain empty in this offline scaffold and are called
out explicitly in validation documentation.
