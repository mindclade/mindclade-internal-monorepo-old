# Bazel remote-cache activation

`activation.json` is the fail-closed source gate for the common-CI GCS action cache. The gateway
implementation and cloud foundations may exist while this contract remains `blocked`; CI then
uses the bounded GitHub-transported disk cache and does not request cache WIF credentials.
The Bazel jobs also omit `id-token: write` in this state, so a skipped authentication step cannot
be bypassed by requesting OIDC directly from another process in the job.

Activation requires a reviewed source change to `qualified-v1` and the server-side repository
variable `BAZEL_REMOTE_CACHE_STATE=qualified-v1`. The contract must name the exact applied bucket,
published module release, exact object generation in the locked production-qualification archive
and its SHA-256, reviewer and review timestamp, and successful cold, warm, duplicate, collision,
corruption, negative-route, and cache-loss checks. Connected evidence must also prove CMEK,
retention, versioning, soft-delete recovery, public-access prevention, access logging, denied
reader writes, and denied writer deletes. Either side missing or disagreeing disables or fails the
cache path; it never falls through to an unreviewed writer.

The qualifying source change must add job-scoped `id-token: write` to the Bazel jobs at the same
time it records `qualified-v1`. Reverting either the source record or the permission disables the
connected path; the server-side variable alone can never mint a token.

Pull requests receive the object-viewer identity and run the gateway in `read` mode. Protected
main, merge-group, and scheduled-nightly routes receive the object-creator identity and run it in
`write` mode. Manual nightly dispatch remains cache-disabled because the dedicated WIF provider
does not authorize that route.
