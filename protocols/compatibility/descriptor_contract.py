#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Descriptor-level protobuf compatibility and maturity governance."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
from typing import Any

from google.protobuf import descriptor_pb2


def _field_type(field: descriptor_pb2.FieldDescriptorProto) -> str:
    if field.type_name:
        return field.type_name.lstrip(".")
    return descriptor_pb2.FieldDescriptorProto.Type.Name(field.type)


def _messages(
    package: str,
    prefix: str,
    values: list[descriptor_pb2.DescriptorProto],
) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for message in values:
        name = ".".join(part for part in (package, prefix, message.name) if part)
        result[name] = {
            "fields": [
                {
                    "name": field.name,
                    "number": field.number,
                    "type": _field_type(field),
                    "label": descriptor_pb2.FieldDescriptorProto.Label.Name(field.label),
                    "oneof": (
                        message.oneof_decl[field.oneof_index].name
                        if field.HasField("oneof_index")
                        else None
                    ),
                    "proto3_optional": field.proto3_optional,
                }
                for field in sorted(message.field, key=lambda item: item.number)
            ],
            "reserved_names": sorted(message.reserved_name),
            "reserved_ranges": [
                [item.start, item.end]
                for item in sorted(message.reserved_range, key=lambda value: value.start)
            ],
        }
        nested_prefix = ".".join(part for part in (prefix, message.name) if part)
        result.update(_messages(package, nested_prefix, list(message.nested_type)))
    return result


def _enums(
    package: str,
    prefix: str,
    values: list[descriptor_pb2.EnumDescriptorProto],
) -> dict[str, Any]:
    return {
        ".".join(part for part in (package, prefix, enum.name) if part): {
            "values": [{"name": value.name, "number": value.number} for value in enum.value],
            "reserved_names": sorted(enum.reserved_name),
            "reserved_ranges": [
                [item.start, item.end]
                for item in sorted(enum.reserved_range, key=lambda value: value.start)
            ],
        }
        for enum in values
    }


def surface(files: list[descriptor_pb2.FileDescriptorProto]) -> dict[str, Any]:
    packages: dict[str, Any] = {}
    for file in sorted(files, key=lambda item: item.name):
        if not file.package.startswith("mindclade."):
            continue
        package = packages.setdefault(
            file.package,
            {"files": [], "messages": {}, "enums": {}, "services": {}},
        )
        package["files"].append(file.name)
        package["messages"].update(_messages(file.package, "", list(file.message_type)))
        package["enums"].update(_enums(file.package, "", list(file.enum_type)))
        for service in file.service:
            name = f"{file.package}.{service.name}"
            package["services"][name] = {
                "methods": [
                    {
                        "name": method.name,
                        "input": method.input_type.lstrip("."),
                        "output": method.output_type.lstrip("."),
                        "client_streaming": method.client_streaming,
                        "server_streaming": method.server_streaming,
                    }
                    for method in service.method
                ]
            }
    return {"schema_version": 2, "packages": packages}


def _load_descriptor_sets(paths: list[Path]) -> list[descriptor_pb2.FileDescriptorProto]:
    by_name: dict[str, descriptor_pb2.FileDescriptorProto] = {}
    for path in paths:
        descriptor_set = descriptor_pb2.FileDescriptorSet()
        descriptor_set.ParseFromString(path.read_bytes())
        for file in descriptor_set.file:
            by_name[file.name] = file
    if not by_name:
        raise ValueError("no protobuf descriptors were loaded")
    return list(by_name.values())


def _deprecated_messages(
    package: str,
    prefix: str,
    values: list[descriptor_pb2.DescriptorProto],
) -> dict[str, bool]:
    result: dict[str, bool] = {}
    for message in values:
        name = ".".join(part for part in (package, prefix, message.name) if part)
        result[name] = message.options.deprecated
        nested_prefix = ".".join(part for part in (prefix, message.name) if part)
        result.update(_deprecated_messages(package, nested_prefix, list(message.nested_type)))
    return result


def _wire_compatibility_placeholders(
    files: list[descriptor_pb2.FileDescriptorProto],
) -> dict[str, bool]:
    messages: dict[str, bool] = {}
    for file in files:
        if file.package.startswith("mindclade."):
            messages.update(_deprecated_messages(file.package, "", list(file.message_type)))
    return {name: deprecated for name, deprecated in messages.items() if name.endswith("Scaffold")}


def _runfiles() -> Path:
    for variable in ("RUNFILES_DIR", "TEST_SRCDIR"):
        if value := os.environ.get(variable):
            return Path(value)
    return Path.cwd()


def _find(name: str) -> Path:
    matches = sorted(path for path in _runfiles().rglob(name) if path.is_file())
    if len(matches) != 1:
        raise ValueError(f"expected one runfile named {name}, found {len(matches)}")
    return matches[0]


def _validate_go_options(proto_root: Path) -> list[str]:
    errors: list[str] = []
    for path in sorted(proto_root.rglob("*.proto")):
        text = path.read_text(encoding="utf-8")
        relative = path.relative_to(proto_root)
        package = relative.parts[1]
        expected = (
            "option go_package = "
            f'"go.mindclade.dev/protocols/gen/go/mindclade/{package}/v1;{package}v1";'
        )
        if expected not in text:
            errors.append(f"{path}: missing canonical go_package")
    return errors


def _validate_governance(
    current: dict[str, Any],
    governance: dict[str, Any],
    compatibility_placeholders: dict[str, bool],
) -> list[str]:
    errors: list[str] = []
    declared_packages = {item["package"]: item for item in governance["surfaces"]}
    actual_packages = set(current["packages"])
    if set(declared_packages) != actual_packages:
        errors.append(
            "maturity package set differs: "
            f"declared={sorted(declared_packages)} actual={sorted(actual_packages)}"
        )
    actual_services = {
        service: details
        for package in current["packages"].values()
        for service, details in package["services"].items()
    }
    policies = governance["rpc_policies"]
    if set(policies) != set(actual_services):
        errors.append(
            "RPC policy service set differs: "
            f"declared={sorted(policies)} actual={sorted(actual_services)}"
        )
    for service, details in actual_services.items():
        if service not in policies:
            continue
        methods = {item["name"] for item in details["methods"]}
        declared = set(policies[service])
        if methods != declared:
            errors.append(
                f"{service}: policy methods differ: declared={sorted(declared)} "
                f"actual={sorted(methods)}"
            )
            continue
        for method, policy in policies[service].items():
            if not policy.get("permission"):
                errors.append(f"{service}/{method}: permission is required")
            default = policy.get("default_deadline_millis", 0)
            maximum = policy.get("maximum_deadline_millis", 0)
            if not 1 <= default <= maximum <= 300_000:
                errors.append(f"{service}/{method}: deadline policy is invalid")
            retry = policy.get("retry", {})
            attempts = retry.get("maximum_attempts", 0)
            if not 1 <= attempts <= 3:
                errors.append(f"{service}/{method}: retry attempts are outside [1, 3]")
            idempotency = policy.get("idempotency")
            if idempotency not in {"read", "required", "stream_required"}:
                errors.append(f"{service}/{method}: idempotency policy is invalid")
            if idempotency != "read" and attempts != 1:
                errors.append(f"{service}/{method}: mutations and streams cannot auto-retry")
            if policy.get("maximum_request_bytes") != 1 << 20:
                errors.append(f"{service}/{method}: request limit must be 1 MiB")
    tombstones = set(governance["removed_symbol_tombstones"])
    declared_placeholders = set(governance["wire_compatibility_placeholders"])
    actual_placeholders = set(compatibility_placeholders)
    if declared_placeholders != actual_placeholders:
        errors.append(
            "wire compatibility placeholder set differs: "
            f"declared={sorted(declared_placeholders)} actual={sorted(actual_placeholders)}"
        )
    nondeprecated = sorted(
        name for name, deprecated in compatibility_placeholders.items() if not deprecated
    )
    if nondeprecated:
        errors.append(f"wire compatibility placeholders must be deprecated: {nondeprecated}")
    overlap = sorted(declared_placeholders & tombstones)
    if overlap:
        errors.append(f"wire compatibility placeholders cannot be tombstoned: {overlap}")
    actual_symbols = {
        symbol
        for package in current["packages"].values()
        for group in ("messages", "enums", "services")
        for symbol in package[group]
    }
    collisions = sorted(tombstones & actual_symbols)
    if collisions:
        errors.append(f"removed symbols were reused: {collisions}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--descriptor-set", action="append", default=[])
    parser.add_argument("--baseline")
    parser.add_argument("--governance")
    parser.add_argument("--proto-root")
    parser.add_argument("--emit-baseline", action="store_true")
    args = parser.parse_args()

    descriptor_paths = [Path(value) for value in args.descriptor_set]
    if not descriptor_paths:
        descriptor_paths = sorted(_runfiles().rglob("*-descriptor-set.proto.bin"))
    descriptor_files = _load_descriptor_sets(descriptor_paths)
    current = surface(descriptor_files)
    if args.emit_baseline:
        print(json.dumps(current, indent=2, sort_keys=True))
        return 0

    baseline_path = Path(args.baseline) if args.baseline else _find("protobuf-v1-descriptor.json")
    governance_path = Path(args.governance) if args.governance else _find("protobuf-surfaces.json")
    proto_root = Path(args.proto_root) if args.proto_root else _find("artifact.proto").parents[3]
    baseline = json.loads(baseline_path.read_text(encoding="utf-8"))
    governance = json.loads(governance_path.read_text(encoding="utf-8"))
    errors: list[str] = []
    if current != baseline:
        errors.append(
            "descriptor surface drifted; review fields, enums, reservations, and RPCs, "
            "then update protobuf-v1-descriptor.json"
        )
    errors.extend(_validate_go_options(proto_root))
    errors.extend(
        _validate_governance(
            current,
            governance,
            _wire_compatibility_placeholders(descriptor_files),
        )
    )
    for error in errors:
        print(f"ERROR: {error}")
    if errors:
        return 1
    print(f"protobuf descriptor governance passed for {len(current['packages'])} packages")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
