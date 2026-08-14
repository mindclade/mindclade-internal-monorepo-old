# Internal RPC Faults

Implementation-only, wire-safe fault extraction shared by `connectx` and
`grpcx`. The package never serializes wrapped causes, stacks, arbitrary fields,
credentials, request bodies, or model inputs.
