# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Hermetic entry point for the upstream MLflow CLI."""

import os
import sys

from mlflow.cli import cli


if __name__ == "__main__":
    if sys.argv[1:] == ["mindclade-db-upgrade"]:
        # Keep credentials out of the Kubernetes Job argv/spec. Click receives the URI only after
        # process start, from the projected Secret environment variable.
        backend_store_uri = os.environ.get("MLFLOW_BACKEND_STORE_URI", "")
        if not backend_store_uri:
            raise SystemExit("MLFLOW_BACKEND_STORE_URI is required for database migration")
        cli.main(args=["db", "upgrade", backend_store_uri], prog_name="mlflow")
    else:
        cli()
