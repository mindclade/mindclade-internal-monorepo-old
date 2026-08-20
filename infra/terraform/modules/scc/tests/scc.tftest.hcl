# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  org_id     = "123456789012"
  project_id = "mc-b-audit-001"
  location   = "EUR4"
}

run "each_notification_gets_its_own_topic" {
  command = plan

  variables {
    notifications = {
      all-findings = {
        description  = "Every active finding."
        filter       = "state=\"ACTIVE\""
        pubsub_topic = { name = "mc-scc-findings" }
      }
      urgent = {
        description  = "HIGH and CRITICAL only."
        filter       = "state=\"ACTIVE\" AND severity=\"HIGH\""
        pubsub_topic = { name = "mc-scc-urgent" }
      }
    }
  }

  # A stream nobody watches and a page nobody can ignore are different things. One shared
  # topic for both is how the urgent channel gets muted.
  assert {
    condition     = google_pubsub_topic.findings["all-findings"].name != google_pubsub_topic.findings["urgent"].name
    error_message = "Each notification config must have its own topic."
  }

  assert {
    condition     = google_scc_notification_config.this["urgent"].streaming_config[0].filter == "state=\"ACTIVE\" AND severity=\"HIGH\""
    error_message = "The filter must reach the streaming config unchanged."
  }
}

run "a_notification_with_an_empty_filter_is_rejected" {
  command = plan

  variables {
    notifications = {
      everything = {
        description  = "No filter."
        filter       = "  "
        pubsub_topic = { name = "mc-scc-all" }
      }
    }
  }

  expect_failures = [var.notifications]
}

run "a_mute_without_a_real_reason_is_rejected" {
  command = plan

  variables {
    mute_configs = {
      noisy = {
        description = "too noisy"
        filter      = "category=\"PUBLIC_IP_ADDRESS\""
        owner       = "cloud-security"
        expiry_time = "2099-01-01T00:00:00Z"
      }
    }
  }

  # A mute with no reason is a finding somebody found inconvenient.
  expect_failures = [var.mute_configs]
}

run "a_mute_with_an_empty_filter_is_rejected" {
  command = plan

  variables {
    mute_configs = {
      blanket = {
        description = "This would silence every finding in the entire organization at once."
        filter      = ""
        owner       = "cloud-security"
        expiry_time = "2099-01-01T00:00:00Z"
      }
    }
  }

  expect_failures = [var.mute_configs]
}

run "a_mute_requires_future_expiry" {
  command = plan

  variables {
    mute_configs = {
      expired = {
        description = "This temporary exception is deliberately already expired for the test."
        filter      = "category=\"PUBLIC_IP_ADDRESS\""
        owner       = "cloud-security"
        expiry_time = "2020-01-01T00:00:00Z"
      }
    }
  }

  expect_failures = [google_scc_mute_config.this["expired"]]
}

run "the_findings_dataset_survives_a_destroy_of_its_contents" {
  command = plan

  variables {
    bigquery_export = {
      dataset_id = "scc_findings"
      location   = "EUR4"
      filter     = "state=\"ACTIVE\""
    }
  }

  assert {
    condition     = google_bigquery_dataset.findings[0].delete_contents_on_destroy == false
    error_message = "A findings dataset that the export identity can empty is one an attacker reaching that identity can erase."
  }
}

run "enablement_commands_are_produced_for_every_service" {
  command = plan

  variables {
    services = {
      "security-health-analytics"  = "ENABLE"
      "container-threat-detection" = "ENABLE"
    }
  }

  # No Terraform resource exists for built-in SCC service enablement. The module hands back
  # the commands rather than reporting a green apply for something it never configured.
  assert {
    condition     = length(output.service_enablement_commands) == 2
    error_message = "Every declared service must produce an enablement command."
  }

  assert {
    condition     = length(output.enabled_services) == 2
    error_message = "enabled_services must list what was intended, for the drift sweep to compare against."
  }
}
