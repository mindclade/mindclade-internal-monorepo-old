#!/usr/bin/env python3
from __future__ import annotations
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
def check(root:Path)->list[str]:
    required=(
      'tools/qualification/rust/qualify.py','tools/qualification/rust/update_lock.sh','security/rust-supply-chain.toml','deny.toml',
      'protocols/compatibility/runtime.toml','configs/qualification/failure_injection.toml','configs/qualification/rust_performance.toml',
      'services/node_agent/src/diagnostics_bundle.rs','libs/rust/runtime_core/src/budget/snapshot.rs','control/orchestration/workload.go',
      'libs/rust/worker_protocol/src/workload.rs','tests/integration/vertical_slices/release_gate.py','tools/qualification/foundation_freeze.py',
    ); errors=[]
    for rel in required:
        if not (root/rel).exists(): errors.append(f'missing hardening contract: {rel}')
    qualify=(root/'tools/qualification/rust/qualify.py').read_text(errors='replace')
    if 'generate-lockfile' in qualify: errors.append('Rust qualification must never mutate Cargo.lock')
    if 'require_committed_lock' not in qualify: errors.append('Rust qualification must require a committed Cargo.lock')
    return errors
def main()->int:
    errors=check(ROOT); [print(e) for e in errors]
    if errors:return 1
    print('foundation hardening contract passed');return 0
if __name__=='__main__':raise SystemExit(main())
