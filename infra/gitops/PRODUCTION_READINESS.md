# GitOps production readiness

**Repository posture:** fail-closed source contract implemented; live deployment unqualified.
**Last repository review:** 2026-08-20.
**Owner:** platform engineering with security and ML platform review.

Source presence is not deployment evidence. The committed state cannot reconcile because every
application is paused and uses an invalid revision. No repository credential, cluster endpoint
other than the in-cluster service, or secret value is stored here.

## Implemented controls

| Control | Repository evidence |
| --- | --- |
| Immutable source policy | Every application requires `SET_EXACT_40_CHAR_COMMIT_SHA`; branches, `HEAD`, mutable tags, and ranges are forbidden |
| Reconciliation gate | `skip-reconcile: "true"`; automated sync, prune, and self-heal are all disabled |
| Environment isolation | Separate bootstrap roots; no Kustomize root combines development, staging, and production |
| Project least privilege | Exact source, in-cluster destination namespaces, and kind allowlists; no wildcard source/destination/kind |
| Project governance | Root application cannot manage `AppProject` and therefore cannot widen its own permissions |
| Credential boundary | No `Secret`; repository registration is external and the checked-in ConfigMap is explicitly non-operative |
| Supply chain | cert-manager URL, bytes, and SHA-256 locked; Kueue/JobSet charts vendor locked dependencies and pin controller images by digest |
| CRD ordering | cert-manager `-30`, operator charts `-20`, foundation `0`, admission `10`/`11`, held ML resources `20`-`22` |
| Workload fail-close | `mindclade-system` retains blocked admission labels, zero Pod quota, held zero-capacity queues, and suspended JobSet canary |
| Destructive-change safety | No application finalizer, no automatic prune, and no automated self-heal in the source baseline |
| Offline verification | `infra/gitops/tests/validate.sh` performs local-only Kustomize, Helm, schema, lock-shape, and policy assertions |

## Activation blockers

The module is not ready for a live cluster until the controlled live repository records and
reviews all of the following:

- an exact Argo CD chart version, artifact digest/provenance, values, CRD compatibility, and
  rendered manifest digest;
- a dedicated Argo CD cluster identity and effective RBAC audit;
- the selected environment and immutable Git commit SHA;
- external repository authentication, rotation owner, expiry monitoring, and denied-access test;
- cert-manager byte-lock verification and webhook readiness evidence;
- destination cluster/context, Kubernetes compatibility, PSS/admission compatibility, and
  reserved system capacity;
- default-deny operator-namespace networking plus reviewed DNS, Kubernetes API, metrics, and
  webhook flows without guessed control-plane CIDRs;
- Argo CD controller HA, backup/restore, network policy, resource bounds, and disaster-recovery
  qualification;
- alerts and dashboards for application error, out-of-sync duration, reconcile latency, source
  fetch failure, webhook failure, certificate expiry, and controller unavailability;
- manual-sync and rollback rehearsals with pruning disabled;
- promotion approval from platform engineering, security, the environment owner, and ML platform
  before either operator or ML resource application is unpaused.

## Automation policy

Initial bootstrap and the first two observed reconciliations are manual. CRD/controller
applications and the root application keep automatic prune disabled permanently unless a
separate CRD retirement and disaster-recovery design is approved. Foundation self-heal may be
enabled only in a controlled live overlay after drift tests prove that emergency response and
break-glass annotations are not overwritten. ML-resource automation remains disabled until queue
capacity, admission, cancellation, checkpoint/restore, numerical parity, and observability gates
are qualified.

## Release evidence

Attach the following to each promotion record:

1. immutable source commit and rendered SHA-256 for the selected bootstrap and environment roots;
2. output of the offline validator at that commit;
3. redacted Argo CD and cluster versions plus CRD stored-version review;
4. `diff` evidence showing no unexpected namespace, cluster-scoped, RBAC, webhook, or Secret
   changes;
5. health evidence after each wave and proof that Pod quota, queues, and JobSet suspension remain
   blocked;
6. the tested rollback commit and recovery owner.
