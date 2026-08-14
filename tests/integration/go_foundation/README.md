# Go foundation consumption qualification

This suite prevents production Go processes from drifting into independent
lifecycle, signal, retry, health, outbox, work-queue, or projection frameworks.
It verifies that every control-plane command uses the shared bootstrap, that the
bootstrap delegates to `servicekit/production.Builder`, and that every public
foundation capability is assigned to at least one concrete process role.

The suite checks architecture; package-level tests continue to verify behavior,
concurrency, fencing, transaction handling, and provider conformance.
