# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Release packaging rules.

Currently one macro: `mindclade_model_bundle`, which publishes model weights as an OCI
artifact so a serving pod can mount them as a Kubernetes image volume.

WHY WEIGHTS ARE AN OCI IMAGE AND NOT A TARBALL IN A BUCKET.

A bucket object has no admission story. Kubernetes will not check it, Binary Authorization
cannot attest it, and the only thing standing between a serving pod and arbitrary weights is
whichever process wrote the path into a config map. Shipping weights as an OCI artifact puts
them on the one plane the cluster already governs: they are pulled by digest, they can carry
attestations, and Gatekeeper can refuse the pod if they do not.

WHY AN IMAGE VOLUME AND NOT AN INIT CONTAINER.

Both were considered; the estate's own constraints decided it. Binary Authorization does not
enforce init containers — a documented limitation — so an `oras`-pull init container is
unenforceable by the control that is supposed to enforce it. OCI VolumeSource (KEP-4639) is
stable in Kubernetes 1.36, which the platform tuple now pins, so the volume is a supported
feature rather than a gate somebody has to remember to enable.

Image volumes are equally invisible to Binary Authorization, which is why
gitops/policy/ carries the enforcement instead. See that repository's policy/README.md.
"""

load("@rules_oci//oci:defs.bzl", "oci_image", "oci_load", "oci_push")
load("@rules_pkg//pkg:tar.bzl", "pkg_tar")

def mindclade_model_bundle(
        name,
        weights,
        mount_path = "/weights",
        annotations = None,
        visibility = None):
    """Package model weights as an OCI artifact mountable as a Kubernetes image volume.

    Produces:
      <name>_layer   pkg_tar of the weight files
      <name>_image   oci_image with NO base — weights and nothing else
      <name>_load    loads into the local daemon for inspection; not part of the release path
      <name>_push    oci_push with a placeholder repository, overridden at run time

    Args:
      name: base target name.
      weights: label -> destination path map, relative to mount_path. A map rather than a
        srcs list because the destination is part of the contract the serving code reads;
        `srcs` would name each file after its Bazel target instead.
      mount_path: where the files sit inside the artifact. The serving pod's volumeMount must
        agree with it — a disagreement is an empty directory, not an error.
      annotations: label of a FILE holding `name=value` lines, one per annotation — NOT a
        dict. That is rules_oci's own signature ("A file containing a dictionary of
        annotations"), and it is the right shape here: the S8 provenance tuple includes values
        that are not known at analysis time (the commit SHA, the dataset manifest digest, the
        eval report digest), so they have to arrive through a generated file rather than a
        Starlark literal. Passing a dict fails with a type error naming `attr.label`.
      visibility: forwarded to the image and push targets.
    """
    if not weights:
        fail("mindclade_model_bundle(%s): `weights` is empty. An artifact with no weights " % name +
             "would build, push, mount, and serve nothing — a failure that surfaces as bad " +
             "outputs rather than as an error.")

    # 0444: read-only for everyone, and no execute bit.
    #
    # Weights are data. The serving process reads them and has no reason to modify them, and
    # nothing in the artifact is meant to be run — the image has no entrypoint and no shell to
    # run one with. A mode that permitted writes would also be a lie about the mount, which
    # Kubernetes makes read-only regardless.
    pkg_tar(
        name = name + "_layer",
        files = {label: mount_path + "/" + dest for label, dest in weights.items()},
        mode = "0444",
    )

    # NO `base`, and this is the point of the macro rather than an omission.
    #
    # `base` is optional in rules_oci. Omitting it means the artifact's filesystem contains the
    # weights and nothing else — no distroless userland, no /etc, no CA bundle, no shell.
    # Basing it on distroless would add a few megabytes of operating system that the kubelet
    # would faithfully mount into the serving pod at /weights, where it is at best noise and at
    # worst a set of binaries inside a container that was supposed to hold only data.
    #
    # There is deliberately no `entrypoint` or `cmd` either. This artifact is never run as a
    # container. If someone tries, it fails immediately instead of starting something.
    oci_image(
        name = name + "_image",
        annotations = annotations,

        # MANDATORY when `base` is unspecified, and rules_oci says so by failing analysis:
        # "'os' and 'architecture' are mandatory when 'base' is unspecified." With a base they
        # would be inherited from it; with no base there is nothing to inherit them from, and
        # an OCI descriptor has to declare a platform.
        #
        # linux/amd64 because that is what the node pools are — the same choice
        # //services/go_vanity makes, and the same reason: publishing a platform nothing pulls
        # costs fetch time and invites a manifest list nothing needs.
        #
        # Worth being clear that this is a formality for weights specifically. Nothing in this
        # artifact executes; it is data mounted read-only. But the kubelet resolves an image
        # volume through the same platform matching as a container image, so a descriptor that
        # declared the wrong platform would fail to pull with an error about architecture,
        # which reads like a build problem rather than a manifest one.
        architecture = "amd64",
        os = "linux",
        tars = [name + "_layer"],
        visibility = visibility,
    )

    oci_load(
        name = name + "_load",
        image = name + "_image",
        repo_tags = [name + ":local"],
    )

    # `repository` is a PLACEHOLDER, overridden at run time — the same shape
    # //services/go_vanity:push uses, and for the same reason: this estate has one registry PER
    # ENVIRONMENT, so promotion is a digest copy between registries rather than a moved tag.
    # Baking one environment's address into the source tree would make promoting to another a
    # code change.
    #
    # `remote_tags` is deliberately unset. Tags are computed per run from the git ref, and
    # nothing downstream reads one anyway — gitops pins the digest.
    oci_push(
        name = name + "_push",
        image = name + "_image",
        repository = "localhost/mindclade/" + name,
        visibility = visibility,
    )
