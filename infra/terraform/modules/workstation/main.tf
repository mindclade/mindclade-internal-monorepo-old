# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  # Google publishes exactly one source range for IAP TCP forwarding. It is a constant, not an
  # environment input, and it is deliberately NOT a variable: a configurable source range is how
  # a rule that exists to admit only IAP eventually admits 0.0.0.0/0.
  iap_tcp_forwarding_source_ranges = ["35.235.240.0/20"]

  network_tag = coalesce(var.network_tag, "${var.name}-iap-ssh")

  data_device_name = "workstation-data"
  data_mount_point = "/mnt/workstation-data"
  local_ssd_mount  = "/mnt/local-ssd"

  baseline_labels = {
    "data-classification" = var.data_classification
    environment           = var.environment
    "managed-by"          = "terraform"
    owner                 = var.owner
    system                = "mindclade"
    workload              = "developer-workstation"
  }

  labels = merge(var.labels, local.baseline_labels)

  # The floor, and only the floor. Cache access is granted on the buckets by their owning modules
  # (see required_cache_grants); artifact registry and anything resembling release authority stay
  # out of the default set entirely.
  required_project_roles = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
  ])

  project_roles = setunion(local.required_project_roles, var.extra_project_roles)

  idle_cycles_before_poweroff = floor((var.idle_shutdown_minutes * 60) / var.idle_check_interval_seconds)

  # Provisioning sources are resolved here, at plan time, rather than branched inside the guest.
  # The script a reviewer reads in the plan is then the script that runs, and where an override is
  # set the public endpoint does not appear in the rendered script at all — a guest-side `if` would
  # leave the public URL sitting in the file on the box for someone to reach for.
  #
  # Each is rendered through an explicit null guard rather than interpolated raw. A null reaching a
  # string template is a hard evaluation error inside this locals block, which fires before the
  # instance precondition that exists to name the mistake — so a caller who set a substituter URI
  # and forgot its key would get "cannot include a null value in a string template" pointing at a
  # heredoc instead of the sentence explaining that the two go together.
  apt_components             = join(" ", coalesce(var.apt_mirror_components, ["main"]))
  nix_installer_url          = coalesce(var.nix_installer_url, "https://nixos.org/nix/install")
  nix_installer_sha256       = var.nix_installer_sha256 == null ? "" : var.nix_installer_sha256
  nix_substituter_public_key = var.nix_substituter_trusted_public_key == null ? "" : var.nix_substituter_trusted_public_key

  startup_script = <<-EOT
    #!/usr/bin/env bash
    set -euo pipefail

    DEVICE="/dev/disk/by-id/google-${local.data_device_name}"
    MOUNT="${local.data_mount_point}"

    # Format only when the disk has no filesystem. This single condition is the difference
    # between "survives a stop" and "silently ate the Nix store on reboot".
    if ! blkid "$${DEVICE}" >/dev/null 2>&1; then
      mkfs.ext4 -m 0 -E lazy_itable_init=0,lazy_journal_init=0 -F "$${DEVICE}"
    fi

    mkdir -p "$${MOUNT}"
    DATA_UUID="$(blkid -s UUID -o value "$${DEVICE}")"

    # nofail: serial console is disabled, so a boot that drops to emergency mode over a missing
    # data disk would be unreachable and unrecoverable through the only access path we have.
    if ! grep -q "$${DATA_UUID}" /etc/fstab; then
      printf 'UUID=%s %s ext4 defaults,discard,nofail 0 2\n' "$${DATA_UUID}" "$${MOUNT}" >> /etc/fstab
    fi
    mount -a

    mkdir -p "$${MOUNT}/nix" /nix
    if ! grep -q '^\S\+ /nix ' /etc/fstab; then
      printf '%s/nix /nix none bind,nofail,x-systemd.requires-mounts-for=%s 0 0\n' \
        "$${MOUNT}" "$${MOUNT}" >> /etc/fstab
    fi
    mount -a

    # Local SSD is ephemeral. It may hold the regenerable Bazel disk cache and nothing else;
    # /nix on scratch means a full closure rebuild after every stop.
    if [ "${var.local_ssd_count}" -gt 0 ] && [ -e /dev/nvme0n1 ]; then
      if ! blkid /dev/nvme0n1 >/dev/null 2>&1; then
        mkfs.ext4 -m 0 -F /dev/nvme0n1
      fi
      mkdir -p "${local.local_ssd_mount}"
      mount -o discard,nofail /dev/nvme0n1 "${local.local_ssd_mount}" || true
      mkdir -p "${local.local_ssd_mount}/bazel-cache"
    fi

    NIX_BACKING="$(findmnt -no SOURCE --target /nix || true)"
    case "$${NIX_BACKING}" in
      /dev/nvme*) echo "refusing to continue: /nix resolved to ephemeral scratch" >&2; exit 1 ;;
    esac

    # THE IDLE TIMER IS INSTALLED BEFORE ANYTHING THAT CAN FAIL, AND THE ORDERING IS THE POINT.
    #
    # Every step after this block needs the public internet, and this module is written for an
    # environment whose firewall denies egress by default. An earlier ordering put the package
    # work first, so under `set -e` the script aborted at `apt-get` and the instance came up
    # reachable over IAP, disk intact, with NO idle timer — billing at full rate until the 03:00
    # stop schedule caught it. The cost control was the first thing lost to the failure it exists
    # to survive.
    #
    # This block needs only coreutils, systemd and procps from the base image, so it completes
    # whether or not the machine can reach a package mirror.

    cat > /usr/local/sbin/mindclade-idle-check <<'IDLE'
    #!/usr/bin/env bash
    set -euo pipefail
    STATE=/run/mindclade-idle-cycles
    THRESHOLD_CYCLES="__CYCLES__"
    LOAD_LIMIT="__LOAD__"

    sessions="$(loginctl list-sessions --no-legend 2>/dev/null | wc -l)"
    load1="$(cut -d' ' -f1 /proc/loadavg)"
    busy=0
    if pgrep -f 'bazel|nix-daemon|nix-build|cargo|pytest|[[:space:]]go[[:space:]]' >/dev/null 2>&1; then
      busy=1
    fi

    if [ "$${sessions}" -gt 0 ] || [ "$${busy}" -eq 1 ] || \
       awk -v a="$${load1}" -v b="$${LOAD_LIMIT}" 'BEGIN{exit !(a>=b)}'; then
      echo 0 > "$${STATE}"
      exit 0
    fi

    count="$(cat "$${STATE}" 2>/dev/null || echo 0)"
    count=$((count + 1))
    if [ "$${count}" -ge "$${THRESHOLD_CYCLES}" ]; then
      echo 0 > "$${STATE}"
      systemctl poweroff
      exit 0
    fi
    echo "$${count}" > "$${STATE}"
    IDLE

    sed -i "s/__CYCLES__/${local.idle_cycles_before_poweroff}/; s/__LOAD__/${var.idle_load_threshold}/" \
      /usr/local/sbin/mindclade-idle-check
    chmod 0755 /usr/local/sbin/mindclade-idle-check

    cat > /etc/systemd/system/mindclade-idle.service <<'SVC'
    [Unit]
    Description=Mindclade workstation idle check
    [Service]
    Type=oneshot
    ExecStart=/usr/local/sbin/mindclade-idle-check
    SVC

    cat > /etc/systemd/system/mindclade-idle.timer <<TMR
    [Unit]
    Description=Mindclade workstation idle check timer
    [Timer]
    OnBootSec=${var.idle_check_interval_seconds}
    OnUnitActiveSec=${var.idle_check_interval_seconds}
    AccuracySec=30
    [Install]
    WantedBy=timers.target
    TMR

    systemctl daemon-reload
    systemctl enable --now mindclade-idle.timer

    # PROVISIONING FROM HERE IS BEST-EFFORT AND DELIBERATELY NON-FATAL.
    #
    # Both fetches below leave the VPC. Where egress is denied by default they fail, and that must
    # not take the instance down with them: the disk is already mounted, IAP SSH already works, and
    # the idle timer above is already armed. A half-provisioned box an operator can reach and which
    # stops billing on its own is strictly better than an aborted script.
    #
    # The status file is the contract with the operator. `gcloud compute ssh` then
    # `cat /var/lib/mindclade-provisioning-status` says exactly which steps completed, so a missing
    # `nix` is diagnosed in one command rather than mistaken for a broken image. The runbook reads
    # this file.
    STATUS=/var/lib/mindclade-provisioning-status
    mkdir -p /var/lib
    : > "$${STATUS}"

    record() { printf '%s %s\n' "$1" "$2" >> "$${STATUS}"; }

    set +e

    export DEBIAN_FRONTEND=noninteractive
    %{if var.apt_mirror_url != null}
    # AN INTERNAL MIRROR IN PLACE OF THE PUBLIC ONE, NOT ALONGSIDE IT. The image ships sources
    # pointing at deb.debian.org and packages.cloud.google.com. Both leave the VPC, and where
    # egress is default-denied both stall until their timeout and then fail the whole `apt-get
    # update` — including for the packages the internal mirror answered for. Leaving them enabled
    # would make the override cosmetic. They are moved aside rather than deleted, so
    # `ls /etc/apt/sources.list.d` still shows an operator exactly what was replaced.
    #
    # The suite is read from the running image instead of being an input. It has to match the image
    # that actually booted; a caller pinning bookworm against a trixie image would write a
    # sources.list that resolves to nothing and have it reported as a mirror failure.
    SUITE="$(. /etc/os-release 2>/dev/null && printf '%s' "$${VERSION_CODENAME:-}")"
    if [ -n "$${SUITE}" ]; then
      mkdir -p /etc/apt/sources.list.d
      for existing in /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; do
        # This script runs on every boot, so it must not disable the file it wrote last boot.
        case "$${existing}" in
          /etc/apt/sources.list.d/mindclade-internal.list) continue ;;
        esac
        [ -f "$${existing}" ] && mv -f "$${existing}" "$${existing}.disabled"
      done
      printf 'deb ${var.apt_mirror_url} %s ${local.apt_components}\n' "$${SUITE}" \
        > /etc/apt/sources.list.d/mindclade-internal.list
      record apt-source internal-mirror
    else
      record apt-source FAILED-no-version-codename
    fi
    %{else}
    record apt-source image-default-public-mirror
    %{endif}
    if apt-get update -y && \
       apt-get install -y --no-install-recommends tmux git curl ca-certificates xz-utils; then
      record packages ok
    else
      record packages FAILED-egress-denied-or-mirror-unreachable
    fi

    # An empty pin means unpinned, which is the default and the prior behaviour. When a pin is set
    # the installer is verified BEFORE it is executed, because it is executed as root: running an
    # installer that failed its own digest check and merely noting it in the status file would make
    # the pin decorative. Each outcome records a distinct reason so an operator is not sent to debug
    # the firewall over a digest mismatch, or the digest over a blocked fetch.
    NIX_INSTALLER_URL="${local.nix_installer_url}"
    NIX_INSTALLER_SHA256="${local.nix_installer_sha256}"

    if command -v nix >/dev/null 2>&1; then
      record nix already-present
    elif ! curl --retry 3 --retry-max-time 120 --max-time 600 -fsSL \
           "$${NIX_INSTALLER_URL}" -o /tmp/nix-install.sh; then
      record nix FAILED-installer-unreachable
    elif [ -n "$${NIX_INSTALLER_SHA256}" ] && \
         ! printf '%s  %s\n' "$${NIX_INSTALLER_SHA256}" /tmp/nix-install.sh | \
           sha256sum -c - >/dev/null 2>&1; then
      record nix FAILED-installer-digest-mismatch
    elif sh /tmp/nix-install.sh --daemon --yes; then
      record nix ok
    else
      record nix FAILED-installer-exited-nonzero
    fi
    rm -f /tmp/nix-install.sh

    # Written unconditionally: it is inert without Nix and correct the moment Nix arrives, so a
    # later manual install needs no second pass.
    mkdir -p /etc/nix
    if ! grep -q '^experimental-features' /etc/nix/nix.conf 2>/dev/null; then
      printf 'experimental-features = nix-command flakes\ntrusted-users = root @google-sudoers\n' \
        >> /etc/nix/nix.conf
    fi
    record nix-conf ok

    %{if var.nix_substituter_uri != null}
    # A caller-supplied substituter, which is the only sanctioned way this machine gets one. It is
    # written as `substituters`, not `extra-substituters`: where egress is default-denied
    # cache.nixos.org is unreachable, and leaving it in the list buys nothing but a per-path
    # timeout before the local build that was going to happen anyway. require-sigs is left at its
    # default, so the trusted key is what makes a served path acceptable — which is why the two
    # inputs are refused unless they are set together.
    if ! grep -q '^substituters' /etc/nix/nix.conf 2>/dev/null; then
      printf 'substituters = %s\ntrusted-public-keys = %s\n' \
        '${var.nix_substituter_uri}' '${local.nix_substituter_public_key}' \
        >> /etc/nix/nix.conf
    fi
    record substituter configured
    %{else}
    # The Nix binary-cache bucket is deliberately NOT configured as a substituter here.
    # nix_binary_cache exports substituter_uri = null and client_activation_contract.enabled =
    # false with reason raw-private-gcs-is-not-a-nix-substituter. Wiring it anyway would
    # contradict a module contract. nix_substituter_uri is the input a reviewed substituter
    # service plugs into, and it stays null until one exists.
    record substituter not-configured-by-design
    %{endif}

    set -e
  EOT

  shutdown_script = <<-EOT
    #!/usr/bin/env bash
    set -euo pipefail
    sync
  EOT
}

resource "google_service_account" "workstation" {
  project         = var.project_id
  account_id      = var.service_account_id
  display_name    = "Mindclade ${var.name} workstation"
  description     = "Dedicated keyless identity for the ${var.name} developer workstation. Holds no signing, publication, or attestation authority."
  disabled        = false
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_project_iam_member" "workstation" {
  for_each = local.project_roles

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.workstation.email}"
}

# A separate resource, not an inline boot disk. This is what survives an instance stop, and
# because google_compute_instance.attached_disk carries no auto_delete, it also survives the
# instance being destroyed and recreated.
resource "google_compute_disk" "data" {
  project = var.project_id
  name    = "${var.name}-data"
  zone    = var.zone
  type    = var.disk_type
  size    = var.data_disk_size_gb
  labels  = local.labels

  disk_encryption_key {
    kms_key_self_link = var.kms_key_name
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_compute_instance" "workstation" {
  project                   = var.project_id
  name                      = var.name
  zone                      = var.zone
  machine_type              = var.machine_type
  deletion_protection       = var.deletion_protection
  allow_stopping_for_update = var.allow_stopping_for_update
  can_ip_forward            = false
  tags                      = [local.network_tag]
  labels                    = local.labels

  boot_disk {
    auto_delete       = true
    kms_key_self_link = var.kms_key_name

    initialize_params {
      image  = var.image
      size   = var.boot_disk_size_gb
      type   = var.disk_type
      labels = local.labels
    }
  }

  # kms_key_self_link is the CMEK field here; disk_encryption_key_raw is the separate
  # customer-supplied-key path. Attaching a CMEK-encrypted disk requires the instance to reference
  # the same key, so this restates the key the disk resource already carries rather than
  # duplicating a secret.
  attached_disk {
    source            = google_compute_disk.data.id
    device_name       = local.data_device_name
    mode              = "READ_WRITE"
    kms_key_self_link = var.kms_key_name
  }

  dynamic "scratch_disk" {
    for_each = range(var.local_ssd_count)

    content {
      interface = "NVME"
    }
  }

  # There is deliberately no access_config block. An empty access_config {} would allocate an
  # ephemeral external address; its absence is what makes this instance private, and the extended
  # organization policy compute.vmExternalIpAccess denies external addresses outright.
  network_interface {
    subnetwork = var.subnetwork
    nic_type   = "GVNIC"
    stack_type = "IPV4_ONLY"
  }

  service_account {
    email  = google_service_account.workstation.email
    scopes = ["https://www.googleapis.com/auth/cloud-platform"]
  }

  shielded_instance_config {
    enable_secure_boot          = true
    enable_vtpm                 = true
    enable_integrity_monitoring = true
  }

  # automatic_restart covers Compute-Engine-initiated terminations only, so it does not undo the
  # guest-initiated idle poweroff below.
  scheduling {
    preemptible         = false
    provisioning_model  = "STANDARD"
    automatic_restart   = true
    on_host_maintenance = "MIGRATE"
  }

  advanced_machine_features {
    enable_nested_virtualization = false
  }

  metadata = merge(var.metadata, {
    "enable-oslogin"         = "TRUE"
    "block-project-ssh-keys" = "TRUE"
    "serial-port-enable"     = "FALSE"
    "startup-script"         = local.startup_script
    "shutdown-script"        = local.shutdown_script
  })

  resource_policies = var.daily_stop_schedule == null ? [] : [google_compute_resource_policy.daily_stop[0].self_link]

  lifecycle {
    precondition {
      condition     = startswith(var.zone, "${var.region}-")
      error_message = "zone must belong to region."
    }

    precondition {
      condition     = var.local_ssd_count == 0 || !startswith(var.machine_type, "t2d-")
      error_message = "t2d machine types do not support Local SSD; choose c2d or n2 when local_ssd_count is greater than zero."
    }

    precondition {
      condition     = var.nix_cache_bucket_name != var.bazel_cache_bucket_name
      error_message = "The Nix and Bazel cache buckets must be distinct; the workstation reads one and writes the other."
    }

    precondition {
      condition     = var.idle_check_interval_seconds * 2 <= var.idle_shutdown_minutes * 60
      error_message = "idle_check_interval_seconds must fit at least twice into idle_shutdown_minutes so the idle counter can observe a trend."
    }

    precondition {
      condition     = var.create_iap_ssh_firewall_rule == false || var.network != null
      error_message = "network is required when this module creates the IAP ingress rule."
    }

    # Cross-input rules live here rather than in a variable validation, matching the rules above.
    precondition {
      condition     = (var.nix_substituter_uri == null) == (var.nix_substituter_trusted_public_key == null)
      error_message = "nix_substituter_uri and nix_substituter_trusted_public_key must be set together. A substituter with no trusted key serves paths the guest cannot verify and this module never relaxes require-sigs; a key with no substituter is inert."
    }

    precondition {
      condition     = var.apt_mirror_components == null || var.apt_mirror_url != null
      error_message = "apt_mirror_components applies only when apt_mirror_url is set. Against the image's own sources.list it would be silently ignored, which reads as a mirror that dropped a component."
    }
  }

  timeouts {
    create = "20m"
    update = "20m"
    delete = "20m"
  }
}

resource "google_compute_firewall" "iap_ssh" {
  count = var.create_iap_ssh_firewall_rule ? 1 : 0

  project       = var.project_id
  name          = "${var.name}-iap-ssh"
  network       = var.network
  direction     = "INGRESS"
  priority      = 1000
  source_ranges = local.iap_tcp_forwarding_source_ranges
  target_tags   = [local.network_tag]

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  log_config {
    metadata = "INCLUDE_ALL_METADATA"
  }
}

resource "google_iap_tunnel_instance_iam_member" "operator" {
  for_each = var.operator_principals

  project  = var.project_id
  zone     = var.zone
  instance = google_compute_instance.workstation.name
  role     = "roles/iap.tunnelResourceAccessor"
  member   = each.value
}

resource "google_compute_instance_iam_member" "os_login" {
  for_each = var.operator_principals

  project       = var.project_id
  zone          = var.zone
  instance_name = google_compute_instance.workstation.name
  role          = var.os_login_role
  member        = each.value
}

# Without serviceAccountUser on the identity the instance carries, OS Login passes IAM and then
# fails at connect. That reads as a broken tunnel and sends the operator to debug the wrong layer.
# Scoped to the service account this module owns, so it crosses no ownership boundary.
resource "google_service_account_iam_member" "operator_act_as" {
  for_each = var.operator_principals

  service_account_id = google_service_account.workstation.name
  role               = "roles/iam.serviceAccountUser"
  member             = each.value
}

# A stop schedule with no matching start schedule, deliberately. A workstation that starts itself
# bills for a machine nobody asked for; the developer starts it.
resource "google_compute_resource_policy" "daily_stop" {
  count = var.daily_stop_schedule == null ? 0 : 1

  project = var.project_id
  name    = "${var.name}-daily-stop"
  region  = var.region

  instance_schedule_policy {
    time_zone = var.schedule_timezone

    vm_stop_schedule {
      schedule = var.daily_stop_schedule
    }
  }
}
