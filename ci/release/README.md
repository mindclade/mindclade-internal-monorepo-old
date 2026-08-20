# Reviewed release requests

Production artifact authority starts with a pull request that adds exactly one
`ci/release/requests/vX.Y.Z.yaml` file. The file is immutable after merge and selects one name
from `targets.yaml`; it cannot supply commands, registry paths, identities, or signing roots.

The `release.yml` push event is GitHub-platform-authenticated and restricted to protected
`main`. It discovers one newly added request, validates it before WIF authentication, and then
runs the ARC canary, build, independent qualification, protected deployment attestation, and
review-only GitOps promotion proposal. There is no `workflow_dispatch`, tag, API dispatch, or
caller-selected SHA authority path.

Version 1 intentionally permits one target per request. GitHub reusable-workflow outputs are
singular; allowing a matrix would make the last completed target silently win. Use separate
release IDs for independently promotable artifacts.

Example:

```yaml
---
apiVersion: release.mindclade.dev/v1alpha1
kind: ReleaseRequest
metadata:
  name: v0.2.0
  changeTicket: PLATFORM-1234
spec:
  targets:
    - name: go-vanity
      rollbackDigest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

Validate without credentials:

```sh
python3 ci/release/release_request.py validate \
  --request ci/release/requests/v0.2.0.yaml \
  --source-sha 0123456789abcdef0123456789abcdef01234567
```

Adding a request does not make the source-only rollout active. Activation still requires the
published `.github` v4 release, exact GitHub runner-group policy, capability-specific WIF,
connected ARC canary evidence, and the GitOps receiver to be ready.
