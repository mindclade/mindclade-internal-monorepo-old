# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package main

import rego.v1

test_missing_context_fails_closed if {
	results := deny with input as {"resource_changes": []}
	some result in results
	result.metadata.code == "POLICY_CONTEXT_INVALID"
}

test_public_principals_detected if {
	is_public_principal("allUsers")
	is_public_principal("allAuthenticatedUsers")
	not is_public_principal("group:readers@example.test")
}

test_primitive_roles_detected if {
	is_primitive_role("roles/owner")
	is_primitive_role("roles/editor")
	is_primitive_role("roles/viewer")
	not is_primitive_role("roles/logging.viewer")
}

test_administrative_roles_detected if {
	is_administrative_role("roles/storage.admin")
	is_administrative_role("roles/resourcemanager.organizationAdmin")
	not is_administrative_role("roles/storage.objectViewer")
}

test_known_iam_member_and_binding_shapes if {
	member := {
		"type": "google_project_iam_member",
		"change": {
			"after": {"member": "group:readers@example.test", "role": "roles/logging.viewer"},
			"after_unknown": {"member": false, "role": false},
		},
	}
	binding := {
		"type": "google_project_iam_binding",
		"change": {
			"after": {"members": ["group:readers@example.test"], "role": "roles/logging.viewer"},
			"after_unknown": {"members": [false], "role": false},
		},
	}
	iam_grant_shape_known(member)
	iam_grant_shape_known(binding)
}

test_authoritative_iam_types_are_forbidden if {
	authoritative_iam_type("google_project_iam_binding")
	authoritative_iam_type("google_storage_bucket_iam_policy")
	not authoritative_iam_type("google_project_iam_member")
}

test_unknown_iam_member_is_not_known if {
	change := {
		"type": "google_project_iam_member",
		"change": {
			"after": {"member": null, "role": "roles/logging.viewer"},
			"after_unknown": {"member": true},
		},
	}
	not iam_grant_shape_known(change)
}

test_unknown_iam_role_is_not_known if {
	change := {
		"type": "google_project_iam_member",
		"change": {
			"after": {"member": "group:readers@example.test", "role": null},
			"after_unknown": {"role": true},
		},
	}
	not iam_grant_shape_known(change)
}

test_partial_unknown_and_malformed_binding_members_are_not_known if {
	partial_unknown := {
		"type": "google_project_iam_binding",
		"change": {
			"after": {"members": ["group:readers@example.test", null], "role": "roles/logging.viewer"},
			"after_unknown": {"members": [false, true]},
		},
	}
	malformed := {
		"type": "google_project_iam_binding",
		"change": {
			"after": {"members": {"unexpected": "group:readers@example.test"}, "role": "roles/logging.viewer"},
			"after_unknown": {},
		},
	}
	not iam_grant_shape_known(partial_unknown)
	not iam_grant_shape_known(malformed)
}

test_known_and_unresolved_authoritative_policy_shapes if {
	known := {
		"type": "google_project_iam_policy",
		"change": {
			"after": {"policy_data": "{\"bindings\":[{\"members\":[\"group:auditors@example.test\"],\"role\":\"roles/logging.viewer\"}]}"},
			"after_unknown": {"policy_data": false},
		},
	}
	unknown := {
		"type": "google_project_iam_policy",
		"change": {
			"after": {"policy_data": null},
			"after_unknown": {"policy_data": true},
		},
	}
	malformed := {
		"type": "google_project_iam_policy",
		"change": {
			"after": {"policy_data": "{\"bindings\":[{\"members\":{},\"role\":null}]}"},
			"after_unknown": {"policy_data": false},
		},
	}
	iam_policy_shape_known(known)
	not iam_policy_shape_known(unknown)
	not iam_policy_shape_known(malformed)
}

test_iam_target_must_be_concrete if {
	known := {
		"type": "google_project_iam_member",
		"change": {
			"after": {"project": "synthetic-project"},
			"after_unknown": {"project": false},
		},
	}
	unknown := {
		"type": "google_project_iam_member",
		"change": {
			"after": {"project": null},
			"after_unknown": {"project": true},
		},
	}
	iam_target_known(known)
	iam_target(known) == "synthetic-project"
	not iam_target_known(unknown)
}

test_wildcards_are_not_exact if {
	not valid_exact_string("google_storage_bucket.*")
	not valid_exact_string("group:?@example.test")
	not valid_exact_string("projects/[any]")
	valid_exact_string("google_storage_bucket.synthetic")
}

test_destructive_action_shapes if {
	destructive_actions(["delete"])
	destructive_actions(["delete", "create"])
	destructive_actions(["create", "delete"])
	not destructive_actions(["update"])
	change_kind(["delete"]) == "delete"
	change_kind(["create", "delete"]) == "replace"
}

test_duration_parsing if {
	duration_seconds(3600) == 3600
	duration_seconds("60m") == 3600
	duration_seconds("24h") == 86400
	duration_seconds("7d") == 604800
}

test_retention_profile_numbers_are_positive_integers if {
	positive_integer(1)
	positive_integer(86400)
	not positive_integer(0)
	not positive_integer(-1)
	not positive_integer(0.001)
	not positive_integer(7.5)
}

test_approval_window_uses_injected_runtime_now if {
	approval_time_valid(
		"2026-08-20T12:00:00Z",
		"2026-08-21T12:00:00Z",
		"2026-08-20T12:30:00Z",
	)
	not approval_time_valid(
		"2026-08-20T12:00:00Z",
		"2026-08-21T12:00:01Z",
		"2026-08-20T12:30:00Z",
	)
	not approval_time_valid(
		"2026-08-20T13:00:00Z",
		"2026-08-20T14:00:00Z",
		"2026-08-20T12:30:00Z",
	)
	not approval_time_valid(
		"2026-08-20T11:00:00Z",
		"2026-08-20T12:00:00Z",
		"2026-08-20T12:30:00Z",
	)
}

test_sql_plan_shape_extracts_labels if {
	change := {
		"type": "google_sql_database_instance",
		"change": {"after": {"settings": [{"user_labels": {"data-classification": "restricted"}}]}},
	}
	classification(change) == "restricted"
}

test_secret_plan_shape_extracts_user_managed_replica if {
	change := {
		"type": "google_secret_manager_secret",
		"change": {"after": {"replication": [{
			"auto": [],
			"user_managed": [{"replicas": [{
				"customer_managed_encryption": [{"kms_key_name": "projects/example/locations/us-central1/keyRings/data/cryptoKeys/secrets"}],
				"location": "us-central1",
			}]}],
		}]}},
	}
	resource_locations(change) == ["us-central1"]
	resource_has_cmek(change)
}

test_artifact_registry_cleanup_plan_shape if {
	change := {
		"type": "google_artifact_registry_repository",
		"change": {"after": {"cleanup_policies": [{
			"action": "DELETE",
			"condition": [{"older_than": "30d"}],
		}]}},
	}
	retention_satisfied(change, {"minimum_seconds": 2592000})
}

test_pubsub_requires_explicit_classification_label if {
	change := {
		"type": "google_pubsub_topic",
		"change": {"after": {
			"labels": {"environment": "test"},
			"message_storage_policy": [{"allowed_persistence_regions": ["us-central1"]}],
		}},
	}
	classification(change) == ""
	provider_deletion_protected(change)
}

test_pubsub_deletion_is_destructive if {
	change := {
		"address": "google_pubsub_topic.synthetic",
		"mode": "managed",
		"type": "google_pubsub_topic",
		"change": {
			"actions": ["delete"],
			"before": {"id": "projects/example/topics/synthetic"},
			"after": null,
		},
	}
	destructive_actions(change.change.actions)
	resource_identifier(change) == "projects/example/topics/synthetic"
}

test_redis_snapshot_period_ordering if {
	snapshot_period_seconds("ONE_HOUR") < snapshot_period_seconds("SIX_HOURS")
	snapshot_period_seconds("SIX_HOURS") < snapshot_period_seconds("TWELVE_HOURS")
	snapshot_period_seconds("TWELVE_HOURS") < snapshot_period_seconds("TWENTY_FOUR_HOURS")
}
