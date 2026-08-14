# Mindclade Go SDK

This directory is an independently publishable Go module. It consumes generated
public API contracts and must not import `mindclade.internal` packages. The
current files reserve the SDK surface; API generation and release publication
remain scaffold work and are not represented as production-complete.

The internal control plane uses the root Go module. Only this public SDK is
permitted to own a nested `go.mod`.
