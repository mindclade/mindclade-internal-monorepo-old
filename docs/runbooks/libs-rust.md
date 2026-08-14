# Rust Foundation Qualification Failure

1. Stop promotion of affected Rust services.
2. Reproduce with the pinned Rust toolchain from Nix.
3. Run `python tools/qualification/rust/qualify.py --mode presubmit`.
4. Do not regenerate locks during qualification; use `tools/qualification/rust/update_lock.sh` only for an intentional dependency change.
5. For protocol, unsafe-code, failure-injection, or performance failures, treat the corresponding evidence lane as blocking.
