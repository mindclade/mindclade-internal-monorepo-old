# Immutable requests

Add one `vX.Y.Z.yaml` file per pull request. Never edit or reuse a merged request. The
`release-authority-paths` ruleset requires platform and security review for this directory.

This directory is armed. `.github/workflows/release.yml` fires on a push to protected `main`
that adds exactly one file here, and it runs the real canary, build, push, attestation, and
promotion proposal. A file added to demonstrate the request shape would start a release. Use
the example in `../README.md` or a case in `../tests/test_release_request.py` instead.
