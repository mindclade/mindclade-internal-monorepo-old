<!-- mindclade-doc: contributing@1 -->

# Contributing to Mindclade · internal monorepo

This repository is a target-state production scaffold with one intentionally
complete implementation surface: the shared Go control-plane foundation and
its representative integrations. A file path reserves a boundary; it is not
proof that the capability is production-ready.

## Development environment

Bazel owns the build graph and Nix owns pinned tools and execution images.
Start in the repository development shell and invoke repository targets rather
than installing tools into the host environment:

```bash
tools/dev/nixw develop .#default
tools/dev/bazelw test //... --config=ci
```

The default shell omits the large interactive `golang.org/x/tools` command suite because
repository automation does not use it. Developers who need `goimports`, `stringer`, or the
other helper binaries can enter the opt-in shell with `tools/dev/nixw develop .#go-tools`.

For the implemented Go foundation, the offline lane is:

```bash
tools/qualification/go/validate.sh offline
```

Connected CI must additionally resolve the pinned provider dependencies and run
PostgreSQL, Redis, GCS, Pub/Sub, Kubernetes, Connect, gRPC, and OpenTelemetry
integration suites.

## Materializing a scaffold boundary

Do not replace a stub merely to make the tree look populated. A path becomes a
live production package only when it has:

1. a named owner in `OWNERS.toml` or the owning package metadata;
2. a stable responsibility and dependency boundary;
3. implementation rather than placeholder behavior;
4. a Bazel target and pinned dependencies;
5. meaningful unit or conformance tests;
6. a README describing ownership, non-responsibilities, determinism, limits,
   and failure behavior;
7. qualification evidence appropriate to its risk and deployment status.

## Language ownership

```text
Go        fleet control plane and durable policy
Rust      online/runtime data plane and node execution
Python    scientific, model, training, inference, and evaluation numerics
TileLang  qualification-gated accelerator kernels
TypeScript browser applications and generated web clients
```

A change that crosses those boundaries requires an ADR or an update to the
accepted boundary ADR. Convenience alone is not sufficient.

## Go contribution rules

- Reuse `libs/go` mechanisms instead of creating service-local lifecycle,
  retry, error, identifier, audit, outbox, inbox, queue, lease, pagination,
  resource-version, signing, transport, or provider abstractions.
- Every production process assembles through
  `libs/go/servicekit/production.Builder`.
- Domain policy belongs under `control/`; deployable wiring belongs under
  `services/`; reusable mechanisms belong under `libs/go` only after at least
  two independent consumers demonstrate the need.
- Never create `common`, `shared`, `helpers`, or `utils` dumping grounds.
- Keep a single root `go.mod` and `go.sum` for internal Go code.
- Use package-local tests and shared conformance suites for providers.

See `libs/go/USAGE.md`, `libs/go/LAYERS.md`, and
`docs/guides/go-service-golden-path.md`.

## Change workflow

1. Add or update the owning design/ADR when changing a durable boundary.
2. Implement the smallest complete vertical slice.
3. Add tests before broadening visibility.
4. Run formatting, focused tests, dependency checks, and the affected Bazel
   graph.
5. Update operational docs, runbooks, and `PRODUCTION_READINESS.md` for any
   deployment-facing change.
6. Include migration, rollback, compatibility, and evidence notes in the pull
   request.

## Security and scientific integrity

Never commit credentials, model-weight secrets, private datasets, hidden
evaluation material, patient information, or proprietary partner data. Treat
all external biological files and model inputs as untrusted. Preserve source,
license, consent, database snapshot, transformation, and artifact provenance.

Report security issues using the private process in `SECURITY.md` rather than a
public issue.


## Contributor authorization and intellectual property

A contribution may be submitted only by a person authorized under a current
written employment, contractor, assignment, or other contribution agreement
with Mindclade, LLC. Before opening or updating a pull request, the contributor
must confirm that:

- they have the right and authority to submit every part of the contribution;
- first-party work is covered by the contributor's controlling written
  agreement with Mindclade, LLC.;
- third-party code, data, models, media, fonts, specifications, and generated
  material are identified with their source, version, license, provenance, and
  required notices;
- the contribution contains no material whose confidentiality, license,
  consent, acceptable-use terms, export controls, or other restrictions
  prohibit submission; and
- the change description and validation evidence are complete and accurate.

By submitting or updating a pull request, the contributor represents that these
statements are true. Submission is not acceptance and does not by itself alter
ownership, grant a license, or replace the controlling written agreement.
Signed commits establish source identity and integrity; they are not a
substitute for the required written agreement.

If authorization or ownership is unclear, stop before submission and use the
legal or contract channel named in the applicable agreement. Do not place
confidential material in a public issue or an unapproved email.
