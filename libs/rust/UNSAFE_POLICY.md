# Unsafe Rust Policy

Unsafe Rust is denied by default across the workspace. It is permitted only in
named leaf adapter crates that require OS/ABI functionality which cannot be
expressed through safe Rust.

## Current exception

`libs/rust/ipc_os` is the only current foundation exception. Its Linux `memfd`
and file-descriptor adapter contains a deliberately small audited unsafe surface
around `libc::memfd_create`, `File::from_raw_fd`, and descriptor `fcntl` calls.
The crate overrides the workspace lint locally and carries `SAFETY.md` with the
ownership and lifetime invariants for each block.

Every unsafe-enabled crate requires:

- a package-local `SAFETY.md` and unsafe-block inventory;
- an explanation of ownership, aliasing, validity, lifetime, and syscall invariants;
- Miri where the code path is supported by Miri;
- ASan/TSan or equivalent sanitizer coverage for native/system boundaries;
- fuzz/property/failure-injection coverage appropriate to the input boundary;
- explicit OWNERS safety approval and a qualification/release tag.

Python ABI, GPU/system bindings, mmap/shared-memory implementations, or other
future exceptions must live in separate leaf adapters and cannot silently weaken
the workspace default.
