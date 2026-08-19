# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# The monorepo's first real container.
#
# Everything downstream of a build keys off the image DIGEST, never a tag: gitops pins by
# digest, Binary Authorization attests a digest, and gitops/.github/workflows/
# verify-attestation.yml refuses any reference in rendered/ that is not one. This file is
# where that digest comes from.
#
# go_vanity was chosen to prove the chain because it is genuinely small and genuinely real —
# stdlib only, one handler, tests beside it — so the parts that fail are the supply-chain
# parts rather than the application.

# ---------------------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------------------
# The go directive in go.mod is 1.26.0. Pinned by digest here for the same reason the
# manifests are: `golang:1.26` moves, and a moved base is a different set of bytes in an
# image whose whole purpose is to be attested.
# golang 1.26 — the tag is recorded here rather than after AS, because Docker parses a
# trailing comment on a FROM line as extra arguments and fails with
# "FROM requires either one or three arguments".
FROM golang@sha256:0d1d3a794be25f809dd2cb3160d8c73276c4056a9f8242a138e908ddeee7b6b6 AS build

WORKDIR /src

# Manifests first, so a source-only change does not re-resolve the module graph.
COPY go.mod go.sum ./
RUN go mod download

COPY services/go_vanity ./services/go_vanity

# CGO_ENABLED=0 for a static binary, because the runtime stage has no libc to link against.
# -trimpath so the build path does not appear in the binary — it is the one input that
# differs between a laptop and a runner, and it is what stops two builds of the same commit
# producing the same bytes.
# -buildvcs=false because the build context carries no .git, and the default `auto` would
# stamp nothing while `true` would fail.
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
RUN go build -buildvcs=false -ldflags="-s -w" -o /out/go_vanity ./services/go_vanity/cmd/go_vanity

# ---------------------------------------------------------------------------------------
# Runtime
# ---------------------------------------------------------------------------------------
# distroless static: no shell, no package manager, no libc. A compromised process has nothing
# to exec, which is the point — and it is why the build stage must produce a static binary.
#
# `nonroot` runs as uid 65532, matching runAsUser in
# infra/kubernetes/planes/dev/base/go-vanity.yaml. The two must agree: a manifest asking for a
# uid the image does not have starts a container that cannot read its own filesystem.
# distroless/static-debian12:nonroot — tag on its own line, same parser reason as above.
FROM gcr.io/distroless/static-debian12@sha256:1b7b9f0f0e0a1d2155f531db587cc48ec26aaf97ab64364225f5bf18a054e66a AS runtime

# Documentation only — Kubernetes takes the port from the manifest. Kept because the manifest
# and the image disagreeing is a silent readiness failure, and this is where a reader looks.
EXPOSE 8080

USER 65532:65532

COPY --from=build /out/go_vanity /usr/local/bin/go_vanity

ENTRYPOINT ["/usr/local/bin/go_vanity"]
