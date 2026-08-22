# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  project_id        = "mc-development-platform"
  project_number    = "123456789012"
  cluster           = "us-central1.mc-development"
  attestor_key_ring = "projects/mc-b-seed/locations/us-central1/keyRings/binauthz"

  attestors = {
    build-attestor = { description = "The build pipeline produced this image." }
  }

  default_admission_rule = {
    evaluation_mode         = "REQUIRE_ATTESTATION"
    enforcement_mode        = "ENFORCED_BLOCK_AND_AUDIT_LOG"
    require_attestations_by = ["build-attestor"]
  }
}

run "attestor_keys_are_asymmetric_sign_and_hsm_by_default" {
  command = plan

  assert {
    condition     = output.project_id == "mc-development-platform"
    error_message = "Consumers require the exact applied attestor project rather than a reconstructed name."
  }

  assert {
    condition     = google_kms_crypto_key.attestor["build-attestor"].purpose == "ASYMMETRIC_SIGN"
    error_message = "An attestor key must be a signing key; a symmetric key cannot produce an attestation."
  }

  assert {
    condition     = google_kms_crypto_key.attestor["build-attestor"].version_template[0].protection_level == "HSM"
    error_message = "Attestor keys default to HSM; software protection is an explicit downgrade."
  }
}

run "require_attestation_with_no_attestors_is_rejected" {
  command = plan

  variables {
    default_admission_rule = {
      evaluation_mode         = "REQUIRE_ATTESTATION"
      enforcement_mode        = "ENFORCED_BLOCK_AND_AUDIT_LOG"
      require_attestations_by = []
    }
  }

  # The single most dangerous misconfiguration available here: it admits every image while
  # reading in the console as the strictest setting there is.
  expect_failures = [var.default_admission_rule]
}

run "a_rule_naming_an_undeclared_attestor_is_rejected" {
  command = plan

  variables {
    default_admission_rule = {
      evaluation_mode         = "REQUIRE_ATTESTATION"
      enforcement_mode        = "ENFORCED_BLOCK_AND_AUDIT_LOG"
      require_attestations_by = ["build-attestor", "vuln-scan-attestor"]
    }
  }

  expect_failures = [google_binary_authorization_policy.this]
}

run "namespace_rules_are_keyed_by_location_cluster_namespace" {
  command = plan

  variables {
    cluster_admission_rules = {
      "gatekeeper-system" = {
        evaluation_mode  = "ALWAYS_ALLOW"
        enforcement_mode = "DRYRUN_AUDIT_LOG_ONLY"
      }
    }
  }

  # Google matches this string literally. A rule whose cluster prefix is wrong applies to
  # nothing and still appears in the policy, which reads as coverage.
  assert {
    condition = anytrue([
      for r in google_binary_authorization_policy.this.cluster_admission_rules :
      r.cluster == "us-central1.mc-development.gatekeeper-system"
    ])
    error_message = "A namespace rule must be keyed <location>.<cluster>.<namespace>."
  }
}

run "namespace_rules_without_a_cluster_are_rejected" {
  command = plan

  variables {
    cluster = null
    cluster_admission_rules = {
      "argocd" = {
        evaluation_mode  = "ALWAYS_ALLOW"
        enforcement_mode = "DRYRUN_AUDIT_LOG_ONLY"
      }
    }
  }

  expect_failures = [google_binary_authorization_policy.this]
}

run "a_wildcard_registry_exemption_is_rejected" {
  command = plan

  variables {
    exempt_images = ["*"]
  }

  expect_failures = [var.exempt_images]
}

run "signers_for_an_unknown_attestor_are_rejected" {
  command = plan

  variables {
    attestor_signers = {
      nonexistent-attestor = ["serviceAccount:sa@p.iam.gserviceaccount.com"]
    }
  }

  expect_failures = [google_binary_authorization_policy.this]
}

run "signers_get_attestor_scoped_permissions_and_declare_create_only_occurrences" {
  command = plan

  variables {
    attestor_signers = {
      build-attestor = ["serviceAccount:release@mc-development-platform.iam.gserviceaccount.com"]
    }
  }

  assert {
    condition = (
      google_container_analysis_note_iam_member.signer_attacher["build-attestor:serviceAccount:release@mc-development-platform.iam.gserviceaccount.com"].role == "roles/containeranalysis.notes.attacher" &&
      google_kms_crypto_key_iam_member.signer["build-attestor:serviceAccount:release@mc-development-platform.iam.gserviceaccount.com"].role == "roles/cloudkms.signerVerifier" &&
      output.required_occurrence_permissions == toset([
        "containeranalysis.occurrences.create",
        "containeranalysis.occurrences.get",
        "containeranalysis.occurrences.list",
      ]) &&
      !contains(output.signer_grants["build-attestor:serviceAccount:release@mc-development-platform.iam.gserviceaccount.com"].roles, "roles/containeranalysis.occurrences.editor")
    )
    error_message = "The module must scope note/key access and require only create/get/list occurrence permissions from the live caller."
  }
}

run "deployer_service_agent_gets_verifier_role" {
  command = plan

  variables {
    attestor_verifiers = {
      build-attestor = ["serviceAccount:service-123456789012@gcp-sa-binaryauthorization.iam.gserviceaccount.com"]
    }
  }

  assert {
    condition     = google_binary_authorization_attestor_iam_member.verifier["build-attestor:serviceAccount:service-123456789012@gcp-sa-binaryauthorization.iam.gserviceaccount.com"].role == "roles/binaryauthorization.attestorsVerifier"
    error_message = "Only deployer Binary Authorization service agents receive the attestor verifier role."
  }
}
