# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "contacts_key_by_parent_and_email_not_by_index" {
  command = plan

  variables {
    contacts = {
      "folders/000000000000" = [
        { email = "platform@mindclade.com", subscriptions = ["TECHNICAL", "SUSPENSION"] },
        { email = "legal@mindclade.com", subscriptions = ["LEGAL"] },
      ]
    }
  }

  # A list-index key would move every contact below an insertion, destroying and recreating
  # contacts that did not change.
  assert {
    condition     = contains(keys(google_essential_contacts_contact.this), "folders/000000000000:legal@mindclade.com")
    error_message = "Contacts must be keyed by parent and email so a reorder is not a replacement."
  }

  assert {
    condition     = length(google_essential_contacts_contact.this) == 2
    error_message = "Both contacts on the folder must be created."
  }
}

run "language_tag_defaults_to_en" {
  command = plan

  variables {
    contacts = {
      "folders/000000000000" = [
        { email = "platform@mindclade.com", subscriptions = ["TECHNICAL"] },
      ]
    }
  }

  assert {
    condition     = google_essential_contacts_contact.this["folders/000000000000:platform@mindclade.com"].language_tag == "en"
    error_message = "language_tag must default rather than be required at every call site."
  }
}

run "all_beside_another_category_is_rejected" {
  command = plan

  variables {
    contacts = {
      "folders/000000000000" = [
        { email = "platform@mindclade.com", subscriptions = ["ALL", "TECHNICAL"] },
      ]
    }
  }

  expect_failures = [var.contacts]
}

run "an_unsubscribed_contact_is_rejected" {
  command = plan

  variables {
    contacts = {
      "folders/000000000000" = [
        { email = "platform@mindclade.com", subscriptions = [] },
      ]
    }
  }

  expect_failures = [var.contacts]
}
