# Bazel GCS cache gateway

This process is the loopback-only protocol boundary between Bazel's HTTP action cache and the
private common-CI Cloud Storage bucket. It is built from the repository's Bazel graph before the
large validation phase, authenticates through Application Default Credentials created by the
dedicated GitHub WIF route, and never exposes cloud credentials or a network listener outside the
runner.

The gateway accepts exact `/ac/<sha256>` and `/cas/<sha256>` keys. CAS uploads must match their
path digest. Every object is staged into a private temporary file bounded to one GiB, hashed, and
published through the Cloud Storage XML API with `x-goog-if-generation-match: 0`; duplicate bytes
are idempotent, while a different payload at an existing key is an immutable-collision failure.
The lightweight transport supplies a server-validated CRC32C on upload, stores the canonical
SHA-256 as immutable custom metadata, pins reads to the observed object generation, and verifies
the complete download against that SHA-256 in a private spool before emitting a successful HTTP
response or any object bytes. GET and PUT share a context-aware staging semaphore; the production
launcher permits two concurrent one-GiB staging files, and exposes peak/wait/cancellation counters
for connected load qualification. It deliberately has no list, update, or delete operation and
does not pull the Cloud Storage SDK/protobuf graph into cache bootstrap. The cache is a performance
input only and never supplies provenance, release, or test verdicts.

`read` mode rejects `PUT` in the process and runs with the object-viewer service account.
`write` mode is available only to protected main, merge-group, and scheduled-nightly WIF routes,
whose service account has additive object-creator plus viewer access and no delete permission.
Lifecycle policy owns expiry. The server binds only to an explicit loopback IP and performs a
GCS read probe before writing its readiness file.

Activation remains separate from source implementation. `ci/bazel_cache/activation.json` must
record connected cold/warm, duplicate-write, negative-route, corruption, and cache-loss evidence
plus a bounded concurrent-staging load run before repository governance may set
`BAZEL_REMOTE_CACHE_STATE=qualified-v1`. Until then, CI keeps the bounded GitHub-transported disk
cache and the Bazel jobs omit OIDC permission entirely.
