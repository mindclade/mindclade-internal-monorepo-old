# Model-weight access security

Model weights are restricted content-addressed artifacts, not ordinary public
blobs.

## Controls

- Go registry/control plane owns release state, entitlement, approval,
  two-person/break-glass policy, and immutable audit receipts.
- A broker issues short-lived, workload-bound artifact grants containing exact
  bundle/digest/prefix scope, region, purpose, policy epoch, and expiry.
- Rust artifact proxy/node agent validates grants locally, verifies object and
  manifest digests, and writes only to protected local cache locations.
- Workload identity, node attestation, network policy, and least-privilege
  object-store credentials constrain the data path.
- Python workers receive a local verified bundle path or descriptor; they do not
  own cloud credentials or weight authorization.
- Logs, crash bundles, metrics, and support tools never include weight bytes or
  long-lived download credentials.
- Access, denial, cache activation, and release use are auditable by tenant,
  principal/workload, bundle digest, node, purpose, and time.

## Revocation

Release withdrawal or credential/key compromise advances the relevant epoch,
stops new grants, drains/revokes affected runtimes as required, invalidates
local cache authorization, and preserves evidence for investigation.
