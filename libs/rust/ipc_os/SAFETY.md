# Unsafe-code safety case

`ipc_os` is a named foundational Rust unsafe exception for OS-level bulk IPC.
The unsafe inventory is deliberately small:

- Linux `memfd_create`: creates the descriptor with `MFD_CLOEXEC` and sealing
  enabled, then converts one successful owned descriptor into `std::fs::File`
  exactly once with `FromRawFd`.
- Linux `fcntl`: permanently seals initialized payloads against writes,
  growth, and truncation; it also adjusts `FD_CLOEXEC` only for a descriptor
  owned by the `MemfdSegment`. Every return value is checked.

No raw pointer from untrusted input is dereferenced.  No mapping is exposed as
an unbounded slice.  Every segment is content-digested, length-bounded, leased,
and owned through RAII. The portable file backend uses create-new publication
and owner-only permissions on Unix. New unsafe blocks require an OWNERS review,
Miri where applicable, and a sanitizer/failure-injection test.
