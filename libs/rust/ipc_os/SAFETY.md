# Unsafe-code safety case

`ipc_os` is the only foundational Rust crate allowed to use unsafe code for
OS-level bulk IPC.  The unsafe inventory is deliberately small:

- Linux `memfd_create`: converts one successful owned file descriptor into
  `std::fs::File` exactly once with `FromRawFd`.
- Linux `fcntl`: adjusts `FD_CLOEXEC` only for a descriptor owned by the
  `MemfdSegment` and checks every return value.

No raw pointer from untrusted input is dereferenced.  No mapping is exposed as
an unbounded slice.  Every segment is content-digested, length-bounded, leased,
and owned through RAII.  New unsafe blocks require an OWNERS review, Miri where
applicable, and a sanitizer/failure-injection test.
