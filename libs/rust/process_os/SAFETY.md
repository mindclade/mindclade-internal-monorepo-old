# Unsafe-code safety case

`process_os` is an OS leaf. Service crates remain `forbid(unsafe_code)`.

The only unsafe operations call `libc::kill` with a range-checked process-group
identifier obtained from an owned `std::process::Child`, or the argument-free
`libc::geteuid` scalar query used for local socket ownership checks. Signal zero
performs existence checking; `SIGTERM` and `SIGKILL` implement bounded shutdown.
No raw pointer, reference, buffer, allocator, callback, or Rust layout crosses
the FFI boundary. `ESRCH` is treated as successful cleanup and all other OS
failures are returned as typed faults.
