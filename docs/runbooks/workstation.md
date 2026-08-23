# Developer workstation runbook

## Status and authority

This runbook operates the private developer workstation defined by
`infra/terraform/modules/workstation`. That module's `variables.tf` and its `shutdown_policy`,
`builder_contract`, `cache_access_contract`, and `qualification_requirements` outputs are the
source of truth; this page explains how to work the machine and why each constraint holds.

The workstation is a build and development host. It holds no signing, publication, or attestation
authority — `extra_project_roles` refuses those roles by name, and `cache_access_contract` records
all three as `false`. Nothing here authorizes a release, a promotion, or a change to any gate. Its
default project role set is the observability floor and nothing else. The role deny list in
`variables.tf` names the reason: this is the same boundary the ARC runner-group split exists to
hold, and a developer box that could sign or publish would collapse it from the other side.

Mock `tftest` runs prove configuration contracts and input rejection. They do not prove tunnel
reachability, disk persistence across a real stop, idle-timer behaviour under a detached build,
NAT egress, or CMEK rotation safety. Treat every claim below about live behaviour as design
intent until the connected evidence in `qualification_requirements` is attached.

## Reaching the box

There is no external address and no open SSH port. The instance carries no `access_config` block,
the extended `compute.vmExternalIpAccess` organization policy denies external addresses outright,
and the only ingress rule admits `35.235.240.0/20` on TCP 22. That range is a module `local`, never
an input: a configurable source range is how a rule that exists to admit only IAP eventually
admits `0.0.0.0/0`.

The single supported path is IAP TCP forwarding. The module emits the exact command as
`ssh_command`:

```bash
gcloud compute ssh <name> --project=<project> --zone=<zone> --tunnel-through-iap
```

Anything that needs a local port — a language server, a web preview — rides the same tunnel rather
than opening a second door:

```bash
gcloud compute ssh <name> --project=<project> --zone=<zone> --tunnel-through-iap \
  -- -L 8080:localhost:8080
```

Three IAM bindings must all be present, and the third is the one that gets forgotten:

| Binding | Role | What its absence looks like |
|---|---|---|
| `google_iap_tunnel_instance_iam_member` | `roles/iap.tunnelResourceAccessor` | the tunnel is refused before SSH starts |
| `google_compute_instance_iam_member` | `roles/compute.osLogin` or `osAdminLogin` | login is rejected after the tunnel opens |
| `google_service_account_iam_member` | `roles/iam.serviceAccountUser` | OS Login satisfies IAM and then fails at connect |

Operator principals are `user:` or `group:` addresses only. Service accounts, domains, wildcards,
and the public principals are rejected by validation — this is the inverse of the cache modules'
rule and deliberately so. Those grant machine access and forbid humans; IAP SSH is a human path,
and a service account holding tunnel access is an unattended door into a box that can reach the
caches.

## tmux is the session host, and mosh is not an option

The reason to reach for mosh — a session that survives a dropped local link — is exactly the
reason this workstation exists. mosh cannot be used here, and the constraint is structural rather
than a matter of taste: mosh carries its session over UDP in the 60000-61000 range, and IAP TCP
forwarding carries TCP only. Nothing in the mosh protocol can traverse the tunnel. Admitting it
would mean an external address plus a UDP firewall rule, which discards the private posture the
module is built around.

The supported pattern is tmux on the instance plus SSH keepalives on the client. The tmux server
is owned by the instance, so it outlives the tunnel, the laptop's network, and the laptop itself:

```bash
tmux new -As build      # create or attach, on the workstation
tmux attach -t build    # after any reconnect
```

On the client, keep the tunnel from being torn down by an idle NAT hop rather than by a real
failure. In `~/.ssh/config`:

```text
Host *
  ServerAliveInterval 30
  ServerAliveCountMax 6
```

The prior defect this replaces is the ordinary one: a laptop lid closing forty minutes into a
Bazel build, the SSH channel dying, and every child process dying with it. With tmux the channel
is disposable — reconnect, reattach, and the build is still running. Nothing else about the
session is durable, so see the poweroff note below before assuming tmux protects you from
everything.

## Starting a stopped instance

The steady state of this machine is off. Start it explicitly:

```bash
gcloud compute instances start <name> --project=<project> --zone=<zone>
```

There is deliberately no start schedule. `shutdown_policy.vm_start_schedule` is `null`, and the
`daily_stop_schedule` resource policy carries a `vm_stop_schedule` with no matching
`vm_start_schedule`. A workstation that starts itself bills for a machine nobody asked for, every
day, including the ones where nobody logs in. The developer starts it; that is the whole cost
model.

Compute Engine re-runs `startup-script` on every boot, not only the first. The script is written to
be safely repeatable — it formats only when `blkid` finds no filesystem, appends to `/etc/fstab`
only when the entry is absent, and installs Nix only when `nix` is not already on `PATH`. Expect
the first minute or two after a start to be spent on `apt-get update` and those checks before the
machine is useful.

## Idle shutdown

A systemd timer, `mindclade-idle.timer`, runs `/usr/local/sbin/mindclade-idle-check` every
`idle_check_interval_seconds` (default 300). The check calls the machine idle only when all three
of the following hold:

- `loginctl list-sessions` reports zero sessions;
- the one-minute load average is below `idle_load_threshold` (default 0.5); and
- no process matches the bounded build pattern `bazel|nix-daemon|nix-build|cargo|pytest|go`.

Any non-idle observation resets the counter in `/run/mindclade-idle-cycles` to zero. Consecutive
idle observations increment it, and at `idle_cycles_before_poweroff` — `floor(idle_shutdown_minutes
* 60 / idle_check_interval_seconds)`, twelve at the defaults — the guest calls `systemctl poweroff`.
A module precondition requires the interval to fit at least twice into the shutdown period, so the
counter always observes a trend rather than a single sample.

The guest powers *itself* off, which requires no IAM at all. The alternative — a `gcloud compute
instances stop` loop — would need `roles/compute.instanceAdmin.v1` on the instance's own identity,
and that role is on the module's explicit deny list. The workstation cannot stop itself through the
API because it is not allowed to hold the authority that would let it stop anything else either.

`scheduling.automatic_restart` does not undo this. That setting covers Compute-Engine-initiated
terminations only; a guest-initiated poweroff stays powered off.

### When it fires during a long build

The failure mode worth understanding is that **a detached tmux session is not, by itself, an idle
guard**. When you disconnect, `systemd-logind` tears down your login session, so
`loginctl list-sessions` can legitimately report zero while your build is still running. What
actually protects a detached build is the load average and the process-name pattern.

That pattern is bounded on purpose — an unbounded "is anything running" check never lets the
machine stop — and the consequence is that work driven by a process it does not name is
unprotected. A long `pnpm`, `uv`, `mypy`, `terraform`, `mkdocs`, or bare `make` run that stays
under the load threshold can be powered off underneath you. Builds invoked through
`tools/dev/nixw` and `tools/dev/bazelw` are covered, because `nix-daemon` and `bazel` are both in
the pattern.

If a poweroff does catch a build:

1. Start the instance and reattach. Nothing in the tmux session survived — tmux state lives in
   RAM, and a poweroff is a poweroff. The *build cache* did survive, because `/nix` and the Bazel
   disk cache are on the persistent data disk, so the rebuild is warm rather than cold.
2. Do not disable the timer to get through the next attempt. Raising `idle_shutdown_minutes`
   through the module input is the reviewed route, and the input is bounded to 15-480 minutes so
   the change cannot become "never". Stopping the unit locally needs `sudo`, which
   `roles/compute.osLogin` does not grant; `os_login_role` is set to `osAdminLogin` only as a
   deliberate, recorded choice.
3. If the same work keeps tripping it, the durable fix is to run it under a driver the bounded
   pattern already names, not to widen the pattern until it stops meaning anything.

## workstation-unexpected-uptime

The cost model assumes the box is off most of the time. Continuous uptime past the reviewed
envelope means the idle path is not doing its job, and the alert exists because nothing external
enforces it — there is no stopping authority anywhere but inside the guest.

Confirm, in this order, before treating it as a fault:

1. Is somebody actually using it? A held `loginctl` session is a correct non-idle observation, not
   a defect. Ask before stopping a machine someone is working on.
2. `systemctl status mindclade-idle.timer` — the timer must be `active (waiting)` and enabled. If
   the unit is missing, the startup script did not complete on the last boot; read its output with
   `journalctl -u google-startup-scripts`.
3. `cat /run/mindclade-idle-cycles` — a counter pinned at zero means something keeps resetting it.
   Compare `uptime` against `idle_load_threshold` and check for a process matching the bounded
   pattern that has been left running, which is the usual answer: an abandoned `bazel` server or a
   `pytest` process that never exited.
4. `systemctl list-timers mindclade-idle.timer` to confirm the last and next elapse.

A stopped instance produces no telemetry, so this signal must never fire on missing data. "Off" is
the desired state; an alert that pages when the machine is off would fire every night by design.

## workstation-data-disk-utilization

The data disk carries `/nix`, the Bazel disk cache, and whatever the working tree accumulates. The
reference `/nix/store` measures 46 GB, which is why the disk defaults to 500 GB rather than
something that looks generous until Bazel and `cargo` arrive. When it fills, builds fail with
`ENOSPC` from whichever tool notices first, which is rarely the tool that consumed the space.

Recover in the order that reclaims the most regenerable bytes first:

```bash
df -h /mnt/workstation-data /nix
du -xh --max-depth=1 /mnt/workstation-data | sort -h | tail
nix store gc
tools/dev/nixw develop .#ci-bazel --command tools/dev/bazelw clean --expunge
cargo clean            # per workspace target directory
```

`nix store gc` and a Bazel `clean --expunge` both cost a cold rebuild and nothing else; delete
those before anything under the working tree. Growing `data_disk_size_gb` is a bounded module input
(200-4000 GB) and is the right answer when the reclaimable set is genuinely small. Never resolve
disk pressure by moving `/nix` — see below.

## The data disk, and what survives a stop

The data disk is a separate `google_compute_disk`, not an inline boot disk, and the instance's
`attached_disk` block carries no `auto_delete`. It therefore survives both a stop and a
destroy/recreate of the instance, and both the disk and the service account carry
`prevent_destroy`.

The startup script formats it **only** when `blkid` reports no filesystem on the device. That
single condition is the difference between "survives a stop" and "silently ate the Nix store". If
you are ever debugging a mount problem, do not reach for `mkfs` — an unmounted disk with an intact
filesystem is recoverable, and a reformatted one is not.

What survives a stop, a start, and an instance recreate:

- `/mnt/workstation-data`, the persistent disk itself;
- `/nix`, which is a bind mount from `/mnt/workstation-data/nix`, ordered ahead of
  `nix-daemon.service`; and
- the Bazel disk cache, when it is placed on the data disk rather than on scratch.

What does not survive: everything on the boot disk, which is `auto_delete = true`. That includes
home directories, `/tmp`, apt state, and any checkout you cloned into `$HOME`. Keep work you care
about on the data disk, or push it. A running tmux session survives a dropped tunnel and does not
survive a poweroff.

The fstab entries use `nofail` deliberately. `serial-port-enable` is `FALSE`, so a boot that
dropped to emergency mode over a missing data disk would be unreachable and unrecoverable through
the only access path that exists. `nofail` trades a degraded boot for a reachable one.

Both disks are CMEK-encrypted with the same key. The Compute Engine service agent must hold
`roles/cloudkms.cryptoKeyEncrypterDecrypter` on that key before any apply, and CMEK rotation
safety for the persistent disk is an open qualification requirement, not a proven property.

## Why /nix must never live on Local SSD

Local SSD is ephemeral. Its contents are discarded when the instance stops, and this instance is
designed to stop constantly. `/nix` on scratch therefore means a full closure rebuild after every
single idle poweroff — and because there is no substituter (next section), that rebuild is
compiled locally rather than fetched.

The startup script enforces this rather than documenting it. After mounting, it resolves the
backing device and refuses to continue if `/nix` landed on NVMe:

```bash
NIX_BACKING="$(findmnt -no SOURCE --target /nix || true)"
case "${NIX_BACKING}" in
  /dev/nvme*) echo "refusing to continue: /nix resolved to ephemeral scratch" >&2; exit 1 ;;
esac
```

Local SSD, when `local_ssd_count` is non-zero, holds the Bazel disk cache and nothing else. That
content is regenerable: losing it costs one cold build, not a closure rebuild. Note also that
`t2d-*` machine types do not support Local SSD at all, and a module precondition rejects that
combination rather than letting it fail at apply.

## The Nix cache bucket is not a substituter

The startup script installs Nix and deliberately does **not** point it at the Nix cache bucket.
`nix_binary_cache` exports `substituter_uri = null` and `client_activation_contract.enabled =
false`, with the reason recorded as `raw-private-gcs-is-not-a-nix-substituter`. Raw private GCS
does not speak the authenticated Nix binary-cache protocol; the `roles/storage.objectViewer` grant
lets tooling read objects out of the bucket, which is not the same thing as a substituter and does
not become one by being configured as if it were.

The operational consequence is that **cold builds are expected**. The first
`tools/dev/nixw develop .#ci` on a freshly formatted data disk compiles its closure locally, and
that is the designed behaviour, not a symptom. It is also precisely why `/nix` is on the
persistent disk: the expensive result is paid for once and then survives every stop.

Do not hand-edit `/etc/nix/nix.conf` to add the bucket as a substituter. It will not authenticate,
and it contradicts a module contract whose activation is supposed to be a reviewed change that
introduces a real substituter service.

## Building the remote-execution base

This workstation is the `x86_64-linux` builder for `remote-execution-base`. That package is gated
`optionalAttrs pkgs.stdenv.hostPlatform.isLinux`, so an `aarch64-darwin` laptop cannot build the
Nix package the CI runner image extends. The `builder_contract` output states the boundaries:

```bash
tools/dev/nixw build .#packages.x86_64-linux.remote-execution-base
```

- `covers` is `["x86_64-linux"]`. `does_not_cover` is `["aarch64-linux"]`; an aarch64-linux builder
  remains separately required and this machine is not it, because Arm machine types are refused by
  validation.
- `attestation_authority` is `false`. This box builds the package and can reproduce its digest. It
  does not sign it, publish it, or attest to it, and no role that would let it do so may be added
  through `extra_project_roles`.

Reproducing the expected digest here is evidence about the build; it is not a release decision.

## Troubleshooting

| Symptom | Cause | Action |
|---|---|---|
| OS Login succeeds at IAM but the connection fails at connect | the principal lacks `roles/iam.serviceAccountUser` on the instance's identity | grant it on the workstation service account; this is the third binding in the table above and the one usually missing. It presents as a broken tunnel and sends you to debug the wrong layer |
| the tunnel is refused before SSH starts | missing `roles/iap.tunnelResourceAccessor`, or the ingress rule was never applied | if `create_iap_ssh_firewall_rule = false`, the estate owns the rule and `required_firewall_rule` publishes its exact contract |
| SSH connects, then `apt` and `nix` hang | no Cloud NAT, or Private Google Access is off | both are prerequisites this module does not create; see `required_network_prerequisites` |
| `sudo` is denied | `os_login_role` is `roles/compute.osLogin` | `osAdminLogin` is a deliberate change, not a default. The startup script already performs every root-requiring boot action |
| instance is `TERMINATED` and unreachable | the idle timer fired, or the daily stop schedule ran | start it; do not add a start schedule |
| `/nix` is empty after a start | the data disk did not mount | inspect `blkid`, `findmnt /nix`, and `journalctl -u google-startup-scripts`. Do **not** reformat |
| builds fail with `ENOSPC` | data disk pressure | see `workstation-data-disk-utilization` above |
| a build dies with no error and the box is off | idle poweroff during a detached run | see "When it fires during a long build" |

Preserve `journalctl -u google-startup-scripts`, `systemctl status mindclade-idle.timer`, and
`findmnt` output before restarting anything. A reboot is the first thing that destroys the evidence
for every startup-script and mount fault above.

## Exit criteria

The workstation is healthy when an approved principal can open a tunnel and log in, `/nix` resolves
to the persistent data disk, the idle timer is active with a counter that moves, the data disk has
reclaimable headroom, and `remote-execution-base` builds to its expected digest.

The module's contracts are qualified only when the connected evidence in
`qualification_requirements` exists: an approved principal admitted and an unapproved one denied; a
stop/start cycle with `/nix` intact and no reformat; the idle timer firing when idle and *not*
firing during a detached tmux build; a reproduced `remote-execution-base` digest; NAT egress
reaching every required source; CMEK rotation leaving the data disk attached; and an observed cost
per idle day that matches the idle-shutdown design.
