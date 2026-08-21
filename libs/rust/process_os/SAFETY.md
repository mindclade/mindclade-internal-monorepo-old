# Unsafe-code safety case

`process_os` is an OS leaf. Service crates remain `forbid(unsafe_code)`, and this
crate exposes no raw descriptor, pointer, borrowed foreign memory, or unsafe API.

## Unsafe inventory and invariants

- `libc::geteuid()` has no arguments, returns a scalar `uid_t`, cannot retain a
  Rust reference, and does not transfer ownership. The result is used only for
  local Unix-socket ownership validation.
- `libc::kill(-process_group, signal)` receives a positive, range-checked
  process-group identifier derived from an owned `std::process::Child`. Negation
  therefore cannot overflow and addresses that process group rather than an
  unrelated single process. Only signal zero, `SIGTERM`, and `SIGKILL` are
  admitted by private wrappers.
- Neither call passes a pointer, reference, buffer, allocator, callback, or Rust
  layout across the C ABI. There are consequently no alignment, initialization,
  aliasing, lifetime, `Send`, `Sync`, or drop invariants delegated to callers.
- Every return code is checked immediately through `last_os_error`. `ESRCH`
  means the group is already absent, `EPERM` means an existence probe found a
  group the process cannot signal, and all other failures become typed faults.

`tests/process_group.rs` exercises isolated group creation, bounded termination,
forced descendant cleanup, and leader reaping on Unix. New unsafe blocks or
signals require an updated inventory, OWNERS safety review, and the repository's
Miri/sanitizer/failure-injection qualification where the syscall is supported.
