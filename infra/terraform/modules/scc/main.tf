# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Security Command Center: which detectors run, where findings go, and what is muted.
#
# Three things are deliberately separate here, because collapsing any two of them produces a
# control that looks present and is not:
#
#   services       what SCC actually detects. Left at the tier default, this changes under
#                  you, and fewer findings is indistinguishable from a healthier estate.
#   notifications  where findings go. One machine-readable feed, one urgent channel. A single
#                  destination for both means the urgent one gets muted within a month.
#   mute_configs   what is knowingly ignored, with a reason, in a file somebody reviews.

resource "google_scc_organization_scc_big_query_export" "this" {
  count = var.bigquery_export == null ? 0 : 1

  name         = "findings-export"
  organization = var.org_id
  dataset      = "projects/${var.project_id}/datasets/${google_bigquery_dataset.findings[0].dataset_id}"
  description  = "Continuous export of SCC findings for joining against the audit dataset."
  filter       = var.bigquery_export.filter
}

resource "google_bigquery_dataset" "findings" {
  count = var.bigquery_export == null ? 0 : 1

  project    = var.project_id
  dataset_id = var.bigquery_export.dataset_id
  location   = coalesce(var.bigquery_export.location, var.location)
  labels     = var.labels

  description = "Security Command Center findings. Written by the SCC export service account, not by anything in this repository."

  # A findings dataset that can be dropped by the export's own service account is one an
  # attacker who reaches that identity can erase. Deletion is a deliberate act elsewhere.
  delete_contents_on_destroy = false
}

# ---------------------------------------------------------------------------------------
# Detectors
# ---------------------------------------------------------------------------------------

resource "google_scc_management_organization_security_center_service" "this" {
  for_each = var.services

  organization              = var.org_id
  name                      = each.key
  location                  = "global"
  intended_enablement_state = each.value
}

# ---------------------------------------------------------------------------------------
# Notification routing
# ---------------------------------------------------------------------------------------

resource "google_pubsub_topic" "findings" {
  for_each = var.notifications

  project = var.project_id
  name    = each.value.pubsub_topic.name
  labels  = var.labels
}

resource "google_scc_notification_config" "this" {
  for_each = var.notifications

  config_id    = each.key
  organization = var.org_id
  description  = each.value.description
  pubsub_topic = google_pubsub_topic.findings[each.key].id

  streaming_config {
    filter = each.value.filter
  }
}

# ---------------------------------------------------------------------------------------
# Mutes
# ---------------------------------------------------------------------------------------
# Every entry states what SCC is reporting and why it is expected. The description is
# required to be substantial in variable validation, because a one-word mute is how a finding
# somebody found inconvenient becomes permanent.

resource "google_scc_mute_config" "this" {
  for_each = var.mute_configs

  mute_config_id = each.key
  parent         = "organizations/${var.org_id}"
  description    = trimspace(each.value.description)
  filter         = each.value.filter
}
