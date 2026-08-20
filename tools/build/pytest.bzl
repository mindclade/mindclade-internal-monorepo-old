# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""A py_test that actually runs pytest.

Written as a macro rather than repeated in each package because the wiring is easy to get
subtly wrong in a way that still passes: a plain `py_test` over a pytest module executes no
test function and exits 0, which is indistinguishable in a green run from a test that ran.
"""

load("@rules_python//python:defs.bzl", "py_test")

def pytest_test(name, srcs, deps = [], data = [], imports = [], **kwargs):
    """Run `srcs` under pytest with the repository's own pytest configuration.

    Args:
      name: target name.
      srcs: pytest modules. Passed to pytest as paths, not executed as scripts.
      deps: libraries under test. `@pypi//pytest` is added here, never by the caller.
      data: extra runfiles. pyproject.toml is added here.
      imports: sys.path roots, relative to this package, as for py_library.
      **kwargs: forwarded to py_test.
    """
    py_test(
        name = name,
        srcs = srcs + ["//tools/build:pytest_runner.py"],
        main = "//tools/build:pytest_runner.py",
        # $(location) so the runner is handed runfiles paths rather than source paths, which
        # are not where the files are when the test executes.
        args = ["$(location %s)" % src for src in srcs],
        data = data + ["//:pyproject.toml"],
        imports = imports,
        deps = deps + ["@pypi//pytest"],
        **kwargs
    )
