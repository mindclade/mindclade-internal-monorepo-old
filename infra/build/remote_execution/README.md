# Buildfarm image authority

Buildfarm 2.17.0 is the pinned Remote Execution API implementation. `images.lock.json`
binds the verified upstream tag object, peeled source commit, multi-platform image indexes,
and AMD64/ARM64 child manifests. `MODULE.bazel` imports those exact digests; tags such as
`latest` are never deployment inputs.

An operator mirrors each index into the organization registry by digest and records the
destination digest in connected release evidence. A registry copy must preserve the source
index digest. This source contract does not assert that a mirror, GKE service, Redis backplane,
or remote Bazel execution currently exists.

Nix separately owns `.#packages.{x86_64-linux,aarch64-linux}.remote-execution-base`. Those
archives provide the action toolchain root and are not replacements for the Buildfarm Java
server/worker images. Bazel owns target selection, execution platforms, parity tests, and
release evidence.
