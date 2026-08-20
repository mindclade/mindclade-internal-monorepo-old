#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Creates the three things dns_hub needs before its first `terraform apply` and
# that Terraform itself cannot create: the project it lives in, the bucket its
# state lives in, and the service account it should run as.
#
# WHY NOT TERRAFORM. Each of these is a chicken-and-egg. The project cannot be
# created by a root that declares it as an input; the state bucket cannot hold
# the state of its own creation; the service account cannot be the identity that
# creates itself. Every estate has this bootstrap step -- the honest thing is to
# make it one idempotent script rather than a list of commands in a README that
# drifts from what was actually run.
#
# SAFE TO RE-RUN. Every step checks for the resource first. Nothing is deleted,
# nothing is overwritten, and a partial failure can be fixed and the script run
# again.

set -euo pipefail

PROJECT_ID="${PROJECT_ID:-mc-common-dns}"
PROJECT_NAME="${PROJECT_NAME:-Mindclade DNS}"
STATE_BUCKET="${STATE_BUCKET:-${PROJECT_ID}-tfstate}"
STATE_LOCATION="${STATE_LOCATION:-US}"
SERVICE_ACCOUNT_ID="${SERVICE_ACCOUNT_ID:-terraform-dns}"

# Optional. An organization or folder to create the project under, and the
# billing account to attach. Without a billing account the Cloud DNS API cannot
# be enabled, so the script stops rather than leaving a project that looks ready
# and is not.
ORGANIZATION_ID="${ORGANIZATION_ID:-}"
FOLDER_ID="${FOLDER_ID:-}"
BILLING_ACCOUNT="${BILLING_ACCOUNT:-}"

ASSUME_YES="${ASSUME_YES:-0}"
for argument in "$@"; do
  case "$argument" in
    -y | --yes) ASSUME_YES=1 ;;
    -h | --help)
      sed -n '3,26p' "$0"
      echo
      echo "Environment: PROJECT_ID PROJECT_NAME STATE_BUCKET STATE_LOCATION"
      echo "             SERVICE_ACCOUNT_ID ORGANIZATION_ID FOLDER_ID BILLING_ACCOUNT"
      exit 0
      ;;
    *)
      echo "unknown argument: $argument" >&2
      exit 2
      ;;
  esac
done

say() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
skip() { printf '    already exists: %s\n' "$1"; }

command -v gcloud >/dev/null || {
  echo "gcloud is required and not on PATH" >&2
  exit 1
}

ACCOUNT="$(gcloud config get-value account 2>/dev/null || true)"
if [ -z "$ACCOUNT" ] || [ "$ACCOUNT" = "(unset)" ]; then
  echo "No active gcloud account. Run: gcloud auth login" >&2
  exit 1
fi

cat <<SUMMARY

  Bootstrapping the DNS host project.

    account          ${ACCOUNT}
    project          ${PROJECT_ID}  (${PROJECT_NAME})
    parent           ${ORGANIZATION_ID:+organization ${ORGANIZATION_ID}}${FOLDER_ID:+folder ${FOLDER_ID}}${ORGANIZATION_ID:-${FOLDER_ID:-none — a standalone project}}
    billing          ${BILLING_ACCOUNT:-none supplied — API enablement will fail without one}
    state bucket     gs://${STATE_BUCKET}  (${STATE_LOCATION}, versioned)
    service account  ${SERVICE_ACCOUNT_ID}@${PROJECT_ID}.iam.gserviceaccount.com

  This creates billable resources. Nothing is deleted or overwritten.

SUMMARY

if [ "$ASSUME_YES" != "1" ]; then
  read -r -p "  Proceed? [y/N] " reply
  case "$reply" in
    [yY] | [yY][eE][sS]) ;;
    *)
      echo "  aborted"
      exit 1
      ;;
  esac
fi

# --------------------------------------------------------------------------
say "Project"
if gcloud projects describe "$PROJECT_ID" >/dev/null 2>&1; then
  skip "project ${PROJECT_ID}"
else
  parent_flags=()
  [ -n "$ORGANIZATION_ID" ] && parent_flags+=(--organization "$ORGANIZATION_ID")
  [ -n "$FOLDER_ID" ] && parent_flags+=(--folder "$FOLDER_ID")
  gcloud projects create "$PROJECT_ID" --name="$PROJECT_NAME" "${parent_flags[@]}"
fi

# --------------------------------------------------------------------------
say "Billing"
if gcloud beta billing projects describe "$PROJECT_ID" \
  --format="value(billingEnabled)" 2>/dev/null | grep -qi true; then
  skip "billing already linked"
elif [ -n "$BILLING_ACCOUNT" ]; then
  gcloud beta billing projects link "$PROJECT_ID" --billing-account="$BILLING_ACCOUNT"
else
  cat >&2 <<'MESSAGE'
    No billing account linked and none supplied.

    Cloud DNS cannot be enabled without one, so this stops here rather than
    leaving a project that looks bootstrapped and is not. Re-run with:

      BILLING_ACCOUNT=XXXXXX-XXXXXX-XXXXXX ./bootstrap.sh

    List them with: gcloud beta billing accounts list
MESSAGE
  exit 1
fi

# --------------------------------------------------------------------------
say "APIs"
# iamcredentials is what makes impersonation work; without it the provider's
# impersonate_service_account fails with a permission error that names the
# token endpoint rather than the missing API.
for api in dns.googleapis.com iamcredentials.googleapis.com storage.googleapis.com; do
  if gcloud services list --enabled --project="$PROJECT_ID" \
    --format="value(config.name)" 2>/dev/null | grep -qx "$api"; then
    skip "$api"
  else
    gcloud services enable "$api" --project="$PROJECT_ID"
  fi
done

# --------------------------------------------------------------------------
say "State bucket"
if gcloud storage buckets describe "gs://${STATE_BUCKET}" >/dev/null 2>&1; then
  skip "gs://${STATE_BUCKET}"
else
  # Versioning is not optional. This state holds the delegation for every domain
  # the company owns; recovering from a bad apply is a restore, and without
  # object versioning there is nothing to restore from.
  #
  # Uniform bucket-level access removes per-object ACLs, so bucket IAM is the
  # only thing that grants access -- one place to read when answering "who can
  # read our Terraform state".
  gcloud storage buckets create "gs://${STATE_BUCKET}" \
    --project="$PROJECT_ID" \
    --location="$STATE_LOCATION" \
    --uniform-bucket-level-access \
    --public-access-prevention
  gcloud storage buckets update "gs://${STATE_BUCKET}" --versioning
fi

# --------------------------------------------------------------------------
say "Terraform service account"
SERVICE_ACCOUNT_EMAIL="${SERVICE_ACCOUNT_ID}@${PROJECT_ID}.iam.gserviceaccount.com"
if gcloud iam service-accounts describe "$SERVICE_ACCOUNT_EMAIL" \
  --project="$PROJECT_ID" >/dev/null 2>&1; then
  skip "$SERVICE_ACCOUNT_EMAIL"
else
  gcloud iam service-accounts create "$SERVICE_ACCOUNT_ID" \
    --project="$PROJECT_ID" \
    --display-name="Terraform — dns_hub" \
    --description="Applies infra/terraform/environments/dns_hub. Impersonated; holds no keys."
fi

# dns.admin rather than editor: this identity's entire job is managing zones and
# records in one project.
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:${SERVICE_ACCOUNT_EMAIL}" \
  --role="roles/dns.admin" --condition=None >/dev/null
gcloud storage buckets add-iam-policy-binding "gs://${STATE_BUCKET}" \
  --member="serviceAccount:${SERVICE_ACCOUNT_EMAIL}" \
  --role="roles/storage.objectAdmin" >/dev/null

# The grant that makes impersonation usable, and the reason NO KEY is ever
# created: the human mints a short-lived token instead of holding a private key
# that lives until someone remembers to rotate it.
gcloud iam service-accounts add-iam-policy-binding "$SERVICE_ACCOUNT_EMAIL" \
  --project="$PROJECT_ID" \
  --member="user:${ACCOUNT}" \
  --role="roles/iam.serviceAccountTokenCreator" >/dev/null

cat <<NEXT

$(say "Done")
    Add to terraform.tfvars:

      project_id                  = "${PROJECT_ID}"
      impersonate_service_account = "${SERVICE_ACCOUNT_EMAIL}"

    Then:

      terraform init -backend-config=bucket=${STATE_BUCKET}
      terraform plan
      terraform apply
      terraform output -json name_servers    # what to paste into the registrar

NEXT
