# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

{
  pkgs,
  ccToolchain,
  name,
  system,
}:

assert pkgs.stdenv.hostPlatform.isLinux;
let
  entrypoint = pkgs.writeShellApplication {
    name = "mindclade-exec-entrypoint";
    runtimeInputs = [ pkgs.coreutils ];
    text = builtins.readFile ../images/entrypoint.sh;
  };
  runtime = [
    pkgs.bashInteractive
    pkgs.bazel_9
    pkgs.cacert
    pkgs.coreutils
    pkgs.findutils
    pkgs.gnugrep
    pkgs.gnutar
    pkgs.gzip
    ccToolchain
    entrypoint
  ];
in
pkgs.dockerTools.buildLayeredImage {
  inherit name;
  tag = "9.1.1-${system}";
  created = "1970-01-01T00:00:01Z";
  maxLayers = 120;
  contents = runtime ++ [ pkgs.dockerTools.fakeNss ];
  extraCommands = ''
    mkdir -p tmp home/nonroot workspace
    chmod 01777 tmp
    chmod 0755 home home/nonroot workspace
  '';
  config = {
    Entrypoint = [ "/bin/mindclade-exec-entrypoint" ];
    Env = [
      "HOME=/home/nonroot"
      "LANG=C.UTF-8"
      "LC_ALL=C.UTF-8"
      "MINDCLADE_CC_TOOLCHAIN_ROOT=${ccToolchain}"
      "PATH=${pkgs.lib.makeBinPath runtime}"
      "TMPDIR=/tmp"
    ];
    Labels = {
      "dev.mindclade.authority" = "nix";
      "dev.mindclade.bazel-version" = "9.1.1";
      "dev.mindclade.system" = system;
      "org.opencontainers.image.source" = "https://github.com/mindclade/mindclade-internal-monorepo";
      "org.opencontainers.image.title" = "Mindclade Bazel remote-execution base";
    };
    User = "65532:65532";
    WorkingDir = "/workspace";
  };
}
