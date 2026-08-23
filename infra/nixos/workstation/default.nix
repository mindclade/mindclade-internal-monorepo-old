# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

{
  flakeLockSha256,
  nixpkgsRevision,
  sourceRevision,
}:
{
  config,
  lib,
  modulesPath,
  pkgs,
  ...
}:
let
  imageContract = {
    schema = "mindclade-workstation-image-v1";
    system = "x86_64-linux";
    variant = "developer-workstation";
    source_sha = sourceRevision;
    flake_lock_sha256 = flakeLockSha256;
    nixpkgs_revision = nixpkgsRevision;
    state_version = config.system.stateVersion;
    runtime_installation = false;
    tool_versions = {
      bazel = pkgs.bazel_9.version;
      git = pkgs.git.version;
      go = pkgs.go.version;
      nix = config.nix.package.version;
      node = pkgs.nodejs_22.version;
      python = pkgs.python3.version;
      rust = pkgs.rustc.version;
    };
  };
  # `builtins.toFile` keeps the small provenance contract materializable on every supported
  # operator host. The disk image remains x86_64-linux-only, but reviewing its contract must not
  # require an x86 builder or silently tempt a reviewer to skip the check on Apple Silicon.
  imageContractFile = builtins.toFile "mindclade-workstation-image-contract.json" (
    builtins.toJSON imageContract + "\n"
  );
  idleCheck = pkgs.writeShellApplication {
    name = "mindclade-idle-check";
    runtimeInputs = with pkgs; [
      coreutils
      gawk
      procps
      systemd
    ];
    text = ''
      state=/run/mindclade-idle-cycles
      threshold_cycles="''${IDLE_CYCLES:-12}"
      load_limit="''${LOAD_LIMIT:-0.5}"

      [[ "$threshold_cycles" =~ ^[1-9][0-9]*$ ]] || {
        echo "invalid IDLE_CYCLES" >&2
        exit 1
      }
      [[ "$load_limit" =~ ^[0-9]+([.][0-9]+)?$ ]] || {
        echo "invalid LOAD_LIMIT" >&2
        exit 1
      }

      sessions="$(loginctl list-sessions --no-legend 2>/dev/null | wc -l)"
      load1="$(cut -d' ' -f1 /proc/loadavg)"
      busy=0
      if pgrep -f 'bazel|nix-daemon|nix-build|cargo|pytest|[[:space:]]go[[:space:]]' \
        >/dev/null 2>&1; then
        busy=1
      fi

      if [ "$sessions" -gt 0 ] || [ "$busy" -eq 1 ] || \
        awk -v a="$load1" -v b="$load_limit" 'BEGIN{exit !(a>=b)}'; then
        echo 0 > "$state"
        exit 0
      fi

      count="$(cat "$state" 2>/dev/null || echo 0)"
      count=$((count + 1))
      if [ "$count" -ge "$threshold_cycles" ]; then
        echo 0 > "$state"
        systemctl poweroff
        exit 0
      fi
      echo "$count" > "$state"
    '';
  };
in
{
  imports = [ "${modulesPath}/virtualisation/google-compute-image.nix" ];

  system.stateVersion = "26.05";
  networking.hostName = "mindclade-workstation";
  networking.firewall.allowedTCPPorts = [ 22 ];

  services.openssh = {
    enable = true;
    settings = {
      KbdInteractiveAuthentication = false;
      PasswordAuthentication = false;
      PermitRootLogin = "no";
    };
  };

  security.sudo.execWheelOnly = true;

  nix = {
    gc = {
      automatic = true;
      dates = "weekly";
      options = "--delete-older-than 30d";
    };
    settings = {
      auto-optimise-store = true;
      experimental-features = [
        "flakes"
        "nix-command"
      ];
      trusted-users = [
        "root"
        "@google-sudoers"
      ];
    };
  };

  environment.etc."mindclade/image-contract.json".source = imageContractFile;
  environment.systemPackages = with pkgs; [
    bazel_9
    cargo
    curl
    e2fsprogs
    gcc
    git
    gnumake
    go
    jq
    nodejs_22
    pnpm
    python3
    rustc
    tmux
    util-linux
  ];

  systemd.services.mindclade-idle = {
    description = "Mindclade workstation idle check";
    serviceConfig = {
      EnvironmentFile = "-/run/mindclade/idle.env";
      ExecStart = "${idleCheck}/bin/mindclade-idle-check";
      Type = "oneshot";
    };
  };

  systemd.timers.mindclade-idle = {
    description = "Mindclade workstation idle check timer";
    wantedBy = [ "timers.target" ];
    timerConfig = {
      AccuracySec = "30s";
      OnBootSec = "5m";
      OnUnitActiveSec = "5m";
      Unit = "mindclade-idle.service";
    };
  };

  assertions = [
    {
      assertion = builtins.match "[a-f0-9]{64}" flakeLockSha256 != null;
      message = "The workstation image requires a lowercase flake.lock SHA-256 digest.";
    }
  ];
}
