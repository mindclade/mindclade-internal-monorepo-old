# Rust Foundation Security

- `unsafe_code = "deny"` is the workspace default; the audited `ipc_os` and `process_os`
  leaf exceptions are governed by `UNSAFE_POLICY.md` and their package-local `SAFETY.md`
  cases.
- Every persisted, parsed, provider, or IPC payload is bounded before material allocation.
- Paths reject absolute roots, traversal, empty components, and platform prefixes at namespace boundaries.
- SHA-256 is verified at object, artifact, checkpoint, record, stream, and bulk-IPC trust boundaries.
- Signed execution/admission authority is locally verified from bounded Ed25519 keysets; Go policy issuance is not a synchronous runtime dependency after admission.
- Error context supports explicit sensitive-value redaction.
- Conditional writes, leases, and fencing/resource versions prevent stale writers from committing.
- Atomic publication and commit markers prevent partially visible checkpoints and artifacts.
- Foundation crates create no ambient async runtime, global thread pool, or hidden provider client; Tokio/provider stacks are explicit runtime/adapter dependencies.

The Python bridge is safe Rust only. Any future native Python ABI shim must live in
an audited adapter package with Miri, sanitizer, and pointer/buffer-lifetime
qualification.
