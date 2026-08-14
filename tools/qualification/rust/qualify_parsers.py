#!/usr/bin/env python3
"""Parser qualification: tests plus bounded corpus/fuzz readiness."""

from common import require_tool, run, verify_toolchain


def main() -> int:
    verify_toolchain()
    cargo = require_tool("cargo")
    for package in ("mindclade_bounded_parse", "mindclade_bio_formats"):
        run([cargo, "test", "-p", package, "--all-targets", "--locked"])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
