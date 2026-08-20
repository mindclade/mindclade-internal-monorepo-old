# Unsafe Rust Policy

Unsafe Rust is denied by default across the workspace. It is permitted only in
named leaf adapter crates that require OS/ABI functionality which cannot be
expressed through safe Rust.

## Current exceptions

- `libs/rust/ipc_os` contains the Linux `memfd` and file-descriptor adapter. Its
  deliberately small unsafe surface wraps `libc::memfd_create`, `File::from_raw_fd`,
  and descriptor `fcntl` calls.
- `libs/rust/process_os` contains Unix process-group signaling. Its unsafe surface
  is limited to checked `libc::kill` calls for signal zero, `SIGTERM`, and `SIGKILL`;
  it passes no pointers or Rust-owned memory across the ABI.

Each exception overrides the workspace lint locally and carries a package-local
`SAFETY.md` with the invariants for every unsafe block.

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
