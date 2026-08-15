# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

locals {
  baseline_labels = {
    "data-classification" = var.data_classification
    environment           = var.environment
    "managed-by"          = "terraform"
    owner                 = var.owner
  }

  # secret id + role + accessor name → one IAM member each. The member string is resolved from
  # workload_identity_bindings rather than supplied directly, so an accessor naming a binding
  # that does not exist fails at plan instead of granting nothing and reporting success.
  iam = merge([
    for secret_id, secret in var.secrets : merge(
      {
        for name in secret.accessors :
        "${secret_id}/accessor/${name}" => {
          secret_id = secret_id
          role      = "roles/secretmanager.secretAccessor"
          name      = name
        }
      },
      {
        for name in secret.version_adders :
        "${secret_id}/version-adder/${name}" => {
          secret_id = secret_id
          role      = "roles/secretmanager.secretVersionAdder"
          name      = name
        }
      },
      {
        for name in secret.viewers :
        "${secret_id}/viewer/${name}" => {
          secret_id = secret_id
          role      = "roles/secretmanager.viewer"
          name      = name
        }
      },
    )
  ]...)

  # Every accessor name referenced by any secret, for the existence check below.
  referenced_bindings = toset(flatten([
    for secret_id, secret in var.secrets : concat(
      tolist(secret.accessors),
      tolist(secret.version_adders),
      tolist(secret.viewers),
    )
  ]))

  rotating_secrets = { for id, s in var.secrets : id => s if s.rotation_period != null }

  # A rotation topic is created only if something rotates, and only if the caller has not
  # supplied its own.
  create_rotation_topic = length(local.rotating_secrets) > 0 && length(var.notification_topics) == 0

  rotation_topics = (
    local.create_rotation_topic
    ? [google_pubsub_topic.rotation[0].id]
    : tolist(var.notification_topics)
  )
}

# The topic rotation events are published to.
#
# Created HERE rather than left to the caller because Secret Manager will not accept a
# rotation schedule without one, and the binding it needs is non-obvious: the topic must grant
# publish to Secret Manager's own service agent, and a rotation configured before that grant
# exists fails at apply with a permission error naming a service account nobody created.
#
# What this does NOT do is rotate anything. Secret Manager emits an event; something has to
# subscribe and perform the rotation. Until that exists, a rotation_period marks a secret as
# due and nothing acts — which is why the precondition below insists on a topic rather than
# letting the schedule be decorative.
resource "google_pubsub_topic" "rotation" {
  count = local.create_rotation_topic ? 1 : 0

  project = var.project_id
  name    = "${var.environment}-secret-rotation"
  labels  = merge(var.labels, local.baseline_labels)
}

resource "google_pubsub_topic_iam_member" "rotation_publisher" {
  count = local.create_rotation_topic ? 1 : 0

  project = var.project_id
  topic   = google_pubsub_topic.rotation[0].name
  role    = "roles/pubsub.publisher"

  # Secret Manager's per-project service agent. It is created lazily by Google the first time
  # the API is used, so a first apply into a fresh project can race it — re-running resolves
  # it, and the error names the account.
  member = "serviceAccount:service-${var.project_number}@gcp-sa-secretmanager.iam.gserviceaccount.com"
}

# The CONTAINERS. Never a version — see the note on var.secrets.
resource "google_secret_manager_secret" "this" {
  for_each = var.secrets

  project             = var.project_id
  secret_id           = each.key
  labels              = merge(var.labels, each.value.labels, local.baseline_labels)
  annotations         = merge(each.value.annotations, { description = substr(each.value.description, 0, 1000) })
  version_destroy_ttl = "${var.version_destroy_delay_days * 86400}s"
  deletion_protection = true
  deletion_policy     = "PREVENT"

  replication {
    dynamic "auto" {
      for_each = length(var.replication.user_managed) == 0 ? [1] : []
      content {
        dynamic "customer_managed_encryption" {
          for_each = var.replication.automatic_kms_key_name == null ? [] : [var.replication.automatic_kms_key_name]
          content {
            kms_key_name = customer_managed_encryption.value
          }
        }
      }
    }

    dynamic "user_managed" {
      for_each = length(var.replication.user_managed) == 0 ? [] : [var.replication.user_managed]
      content {
        dynamic "replicas" {
          for_each = user_managed.value
          content {
            location = replicas.value.location
            dynamic "customer_managed_encryption" {
              for_each = replicas.value.kms_key_name == null ? [] : [replicas.value.kms_key_name]
              content {
                kms_key_name = customer_managed_encryption.value
              }
            }
          }
        }
      }
    }
  }

  dynamic "topics" {
    for_each = local.rotation_topics
    content {
      name = topics.value
    }
  }

  dynamic "rotation" {
    for_each = each.value.rotation_period == null ? [] : [each.value.rotation_period]
    content {
      rotation_period = rotation.value
      # next_rotation_time is deliberately omitted. Supplying a computed timestamp makes every
      # plan show a diff on a field nothing intends to change, and the API derives a first
      # rotation from the period on its own.
    }
  }

  lifecycle {
    prevent_destroy = true

    # A rotation period only emits an EVENT. Without a topic carrying it to a handler, the
    # schedule is decorative — the secret is marked as due for rotation and nothing rotates it.
    precondition {
      condition     = each.value.rotation_period == null || length(local.rotation_topics) > 0
      error_message = "Secret \"${each.key}\" declares a rotation_period but no topic carries the event. Secret Manager only emits it; something else has to act on it."
    }

    precondition {
      condition = var.data_classification != "restricted" || (
        length(var.replication.user_managed) == 0 ?
        var.replication.automatic_kms_key_name != null :
        alltrue([for r in var.replication.user_managed : r.kms_key_name != null])
      )
      error_message = "Restricted secrets require CMEK on automatic replication or on every user-managed replica."
    }

    precondition {
      condition = length(setsubtract(
        local.referenced_bindings,
        keys(var.workload_identity_bindings),
      )) == 0
      error_message = "A secret names an accessor with no entry in workload_identity_bindings. The grant would resolve to no principal, and the apply would succeed while granting nothing."
    }
  }
}

resource "google_secret_manager_secret_iam_member" "this" {
  for_each = local.iam

  project   = var.project_id
  secret_id = google_secret_manager_secret.this[each.value.secret_id].secret_id
  role      = each.value.role

  # project NUMBER for the principal, project ID for the pool. The same project named two ways
  # in one string — see the note on var.project_number. Getting it wrong produces a member IAM
  # accepts and no workload matches: the apply succeeds and every read is denied.
  member = format(
    "principal://iam.googleapis.com/projects/%s/locations/global/workloadIdentityPools/%s.svc.id.goog/subject/ns/%s/sa/%s",
    var.project_number,
    var.project_id,
    var.workload_identity_bindings[each.value.name].namespace,
    var.workload_identity_bindings[each.value.name].service_account,
  )
}
