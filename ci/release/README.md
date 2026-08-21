# Reviewed release requests

Production artifact authority starts with a pull request that adds exactly one
`ci/release/requests/vX.Y.Z.yaml` file. The file is immutable after merge and selects one name
from `targets.yaml`; it cannot supply commands, registry paths, identities, or signing roots.

The `release.yml` push event is GitHub-platform-authenticated and restricted to protected
`main`. It discovers one newly added request, validates it before WIF authentication, and then
runs the ARC canary, build, independent qualification, protected deployment attestation, and
review-only GitOps promotion proposal. There is no `workflow_dispatch`, tag, API dispatch, or
caller-selected SHA authority path.

Version `v1beta2` intentionally permits one target per request and makes the rollback strategy
explicit. Target catalog schema 2 binds
that name to a release kind, GitOps application, rollout class, named image build/push targets,
typed non-image artifact slots, and fixed qualification targets. GitHub reusable-workflow outputs
remain singular; use separate release IDs for independently promotable subjects.

Every request names the exact previous release ID and subject digest. The pair is rollback
lineage, not merely an image hint, and cannot be replaced with a mutable tag or a zero digest.

Example:

```yaml
---
apiVersion: release.mindclade.dev/v1beta2
kind: ReleaseRequest
metadata:
  name: v0.2.0
  changeTicket: PLATFORM-1234
spec:
  target: go-vanity
  rollback:
    strategy: previous-release
    previousRelease:
      id: v0.1.0
      subjectDigest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

Validate without credentials:

```sh
python3 ci/release/release_request.py validate \
  --request ci/release/requests/v0.2.0.yaml \
  --source-sha 0123456789abcdef0123456789abcdef01234567
```

Adding a request does not make the source-only rollout active. Production activation requires the
reviewed `.github` v5 release before the exact previous-release ID and subject-digest pair
can cross the promotion boundary, plus exact runner-group policy, capability-specific WIF,
connected ARC canary evidence, and a ready GitOps receiver.
