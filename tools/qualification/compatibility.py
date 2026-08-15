#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Runtime rolling-compatibility matrix policy."""

from __future__ import annotations

import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
REQUIRED_EDGES = {
    "control_to_gateway",
    "gateway_to_host",
    "host_to_model_worker",
    "artifact_control_to_proxy",
    "training_to_node_checkpoint",
}
REQUIRED_FIELDS = {
    "name",
    "producer",
    "consumer",
    "contract",
    "writer_schema",
    "reader_minimum",
    "reader_maximum",
    "n_minus_one_required",
}


def main() -> int:
    data = tomllib.loads((ROOT / "protocols/compatibility/runtime.toml").read_text())
    edges = data.get("edge", [])
    errors = []
    names = {e.get("name") for e in edges}
    if missing := REQUIRED_EDGES - names:
        errors.append(f"missing compatibility edges: {sorted(missing)}")
    if len(names) != len(edges):
        errors.append("duplicate compatibility edge names")
    for edge in edges:
        missing = REQUIRED_FIELDS - set(edge)
        if missing:
            errors.append(f"{edge.get('name')}: missing fields {sorted(missing)}")
            continue
        writer = int(edge["writer_schema"])
        minimum = int(edge["reader_minimum"])
        maximum = int(edge["reader_maximum"])
        if min(writer, minimum, maximum) <= 0 or minimum > writer or writer > maximum:
            errors.append(f"{edge['name']}: invalid schema range")
    components = {
        c["name"]: c
        for c in tomllib.loads((ROOT / "components.toml").read_text()).get("component", [])
    }
    productionish = {"qualified", "production"}
    for edge in edges:
        producer = components.get(edge.get("producer"))
        consumer = components.get(edge.get("consumer"))
        if (
            producer
            and consumer
            and (producer.get("status") in productionish or consumer.get("status") in productionish)
        ):
            if not edge.get("n_minus_one_required"):
                errors.append(
                    f"{edge['name']}: N-1 compatibility is mandatory once either endpoint is qualified"
                )
            if int(edge["reader_minimum"]) >= int(edge["writer_schema"]):
                errors.append(f"{edge['name']}: N-1 reader range is not represented")
    if errors:
        print("\n".join(errors))
        return 1
    print(f"runtime compatibility matrix passed ({len(edges)} rolling edges)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
