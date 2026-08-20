# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# Written against the map interface this module actually has.
#
# It used to pass `secret_id`, `user_managed_replicas`, `accessor_members`, `viewer_members`,
# `annotations` and `rotation` as top-level variables — a shape this module has not had since
# it became one call covering every secret in a project. Every run failed on "No value for
# required variable: project_number", and the suite was red, so `terraform test` was never
# wired into CI here and nothing said why.
#
# What the module actually takes: `secrets` keyed by secret id, with `accessors` and
# `version_adders` naming keys of `workload_identity_bindings` rather than raw IAM members;
# `replication` shared across all of them; and `project_number` alongside `project_id`,
# because an IAM member string needs the number and the pool needs the id.

mock_provider "google" {}

run "metadata_only_secret_contract" {
  command = plan

  variables {
    project_id     = "mindclade-production"
    project_number = "482910385712"
    environment    = "production"
    owner          = "cloud-platform"

    notification_topics = ["projects/mindclade-production/topics/secret-rotation"]

    replication = {
      user_managed = [
        {
          location     = "us-central1"
          kms_key_name = "projects/security/locations/us-central1/keyRings/secrets/cryptoKeys/control-plane"
        },
        {
          location     = "us-east1"
          kms_key_name = "projects/security/locations/us-east1/keyRings/secrets/cryptoKeys/control-plane"
        },
      ]
    }

    workload_identity_bindings = {
      runtime = {
        namespace       = "serving"
        service_account = "runtime"
      }
    }

    secrets = {
      control-plane-database = {
        description        = "Connection string for the control-plane Cloud SQL instance."
        accessors          = ["runtime"]
        rotation_period    = "7776000s"
        next_rotation_time = "2099-01-01T00:00:00Z"
      }
    }
  }

  assert {
    condition = (
      google_secret_manager_secret.this["control-plane-database"].deletion_protection == true &&
      google_secret_manager_secret.this["control-plane-database"].deletion_policy == "PREVENT" &&
      google_secret_manager_secret.this["control-plane-database"].version_destroy_ttl == "604800s"
    )
    error_message = "Secret and version deletion safeguards must remain enabled."
  }

  assert {
    condition     = length(google_secret_manager_secret.this["control-plane-database"].replication[0].user_managed[0].replicas) == 2
    error_message = "The user-managed production example must retain both regional replicas."
  }

  assert {
    condition     = google_secret_manager_secret_iam_member.this["control-plane-database/accessor/runtime"].role == "roles/secretmanager.secretAccessor"
    error_message = "Runtime access must use the payload-specific accessor role."
  }
}

# A rotation period only emits an EVENT; something else has to act on it. The module refuses
# to leave that dangling — rather than failing when no topic is supplied, it creates one and
# grants Secret Manager's service agent publish on it, because a rotation schedule configured
# before that grant exists fails at apply naming a service account nobody created.
#
# So the assertion is that the topic is created and attached, not that the plan is rejected.
# The precondition on the resource covers the case where that creation is bypassed.
run "rotation_without_a_topic_gets_one" {
  command = plan

  variables {
    project_id     = "mindclade-development"
    project_number = "482910385712"
    environment    = "development"
    owner          = "cloud-platform"

    # Deliberately empty: this is the input that triggers the module-managed topic.
    notification_topics         = []
    rotation_topic_kms_key_name = "projects/security/locations/global/keyRings/secrets/cryptoKeys/rotation-events"

    replication = {
      user_managed = [{ location = "us-central1" }]
    }

    secrets = {
      rotation-canary = {
        description        = "Proves the rotation event has somewhere to go."
        rotation_period    = "2592000s"
        next_rotation_time = "2099-01-01T00:00:00Z"
      }
    }
  }

  assert {
    condition = (
      length(google_pubsub_topic.rotation) == 1 &&
      google_pubsub_topic.rotation[0].kms_key_name == "projects/security/locations/global/keyRings/secrets/cryptoKeys/rotation-events"
    )
    error_message = "A rotating secret with no caller-supplied topic must get a module-managed one."
  }

  assert {
    condition     = length(google_pubsub_topic_iam_member.rotation_publisher) == 1
    error_message = "The rotation topic must grant publish to Secret Manager's service agent, or the schedule fails at apply."
  }


  assert {
    condition = (
      output.required_rotation_topic_kms_grant.member == "serviceAccount:service-482910385712@gcp-sa-pubsub.iam.gserviceaccount.com" &&
      output.required_rotation_topic_kms_grant.role == "roles/cloudkms.cryptoKeyEncrypterDecrypter"
    )
    error_message = "The key-owning state must receive the exact Pub/Sub service-agent CMEK grant."
  }

  assert {
    condition     = length(google_secret_manager_secret.this["rotation-canary"].topics) == 1
    error_message = "The rotating secret must be attached to the rotation topic."
  }
}

# A secret that does NOT rotate needs no topic, and creating one anyway would leave an unused
# Pub/Sub resource in every project.
run "no_rotation_creates_no_topic" {
  command = plan

  variables {
    project_id     = "mindclade-development"
    project_number = "482910385712"
    environment    = "development"

    rotation_topic_kms_key_name = "projects/security/locations/global/keyRings/secrets/cryptoKeys/rotation-events"
    owner                       = "cloud-platform"

    replication = {
      user_managed = [{ location = "us-central1" }]
    }

    secrets = {
      static-secret = {
        description = "Written once, out of band, and never rotated."
      }
    }
  }

  assert {
    condition     = length(google_pubsub_topic.rotation) == 0
    error_message = "No secret rotates, so no rotation topic should exist."
  }
}

run "reject_restricted_secret_without_cmek" {
  command = plan

  variables {
    project_id          = "mindclade-production"
    project_number      = "482910385712"
    environment         = "production"
    owner               = "cloud-platform"
    data_classification = "restricted"

    # A replica with no kms_key_name. Google-managed encryption on a restricted secret is
    # accepted by the API and violates the residency and key-custody rules the classification
    # exists to enforce.
    replication = {
      user_managed = [{ location = "us-central1" }]
    }

    secrets = {
      restricted-without-cmek = {
        description = "Restricted payload with no customer key behind it."
      }
    }
  }

  expect_failures = [google_secret_manager_secret.this["restricted-without-cmek"]]
}

# A principal that can both read a secret and write new versions of it can rotate the secret
# to a value it chooses and read it back.
run "reject_shared_accessor_and_version_adder" {
  command = plan

  variables {
    project_id     = "mindclade-development"
    project_number = "482910385712"
    environment    = "development"
    owner          = "cloud-platform"

    replication = {
      user_managed = [{ location = "us-central1" }]
    }

    workload_identity_bindings = {
      rotation = {
        namespace       = "platform"
        service_account = "rotation"
      }
    }

    secrets = {
      shared-identity-canary = {
        description    = "One principal on both sides of the rotation boundary."
        accessors      = ["rotation"]
        version_adders = ["rotation"]
      }
    }
  }

  expect_failures = [var.secrets]
}

# An accessor naming a binding that does not exist resolves to no principal. The apply
# succeeds, the grant is empty, and every read is denied with nothing pointing here.
run "reject_accessor_with_no_binding" {
  command = plan

  variables {
    project_id     = "mindclade-development"
    project_number = "482910385712"
    environment    = "development"
    owner          = "cloud-platform"

    replication = {
      user_managed = [{ location = "us-central1" }]
    }

    workload_identity_bindings = {}

    secrets = {
      dangling-accessor = {
        description = "Names an accessor nothing declares."
        accessors   = ["runtime"]
      }
    }
  }

  expect_failures = [google_secret_manager_secret.this["dangling-accessor"]]
}

run "reject_rotation_period_without_seconds_suffix" {
  command = plan

  variables {
    project_id     = "mindclade-development"
    project_number = "482910385712"
    environment    = "development"
    owner          = "cloud-platform"

    notification_topics = ["projects/mindclade-development/topics/secret-rotation"]

    replication = {
      user_managed = [{ location = "us-central1" }]
    }

    secrets = {
      rotation-format-canary = {
        description        = "A duration the API will not parse."
        rotation_period    = "90d"
        next_rotation_time = "2099-01-01T00:00:00Z"
      }
    }
  }

  expect_failures = [var.secrets]
}

run "reject_automatic_kms_alongside_user_managed_replicas" {
  command = plan

  variables {
    project_id     = "mindclade-production"
    project_number = "482910385712"
    environment    = "production"
    owner          = "cloud-platform"

    replication = {
      user_managed           = [{ location = "us-central1" }]
      automatic_kms_key_name = "projects/security/locations/global/keyRings/secrets/cryptoKeys/control-plane"
    }

    secrets = {
      conflicting-replication = {
        description = "Automatic and user-managed replication at once."
      }
    }
  }

  expect_failures = [var.replication]
}

run "reject_rotation_without_next_time" {
  command = plan

  variables {
    project_id     = "mindclade-development"
    project_number = "482910385712"
    environment    = "development"

    rotation_topic_kms_key_name = "projects/security/locations/global/keyRings/secrets/cryptoKeys/rotation-events"

    replication = {
      user_managed = [{ location = "us-central1" }]
    }

    secrets = {
      incomplete-rotation = {
        description     = "A rotation period without the timestamp required by the API."
        rotation_period = "2592000s"
      }
    }
  }

  expect_failures = [var.secrets]
}

run "reject_reserved_annotation" {
  command = plan

  variables {
    project_id     = "mindclade-development"
    project_number = "482910385712"
    environment    = "development"

    replication = {
      user_managed = [{ location = "us-central1" }]
    }

    secrets = {
      invalid-annotation = {
        description = "The module owns the description annotation."
        annotations = { description = "caller override" }
      }
    }
  }

  expect_failures = [var.secrets]
}
