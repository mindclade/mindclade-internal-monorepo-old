# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

package main

import rego.v1

# This policy consumes `terraform show -json` output. Runtime policy data is
# intentionally supplied separately so that environment decisions and approvals
# cannot be smuggled into the plan being evaluated.

required_data_types := {
	"google_artifact_registry_repository",
	"google_pubsub_topic",
	"google_redis_instance",
	"google_secret_manager_secret",
	"google_sql_database_instance",
	"google_storage_bucket",
}

required_classifications := {"public", "internal", "confidential", "restricted"}
approval_max_validity_ns := 86400000000000

profile_fields := {
	"schema_version",
	"profile_id",
	"approved_locations",
	"required_labels",
	"data_resource_types",
	"classifications",
}

admin_approval_fields := {
	"kind",
	"address",
	"resource",
	"member",
	"role",
	"ticket",
	"owner",
	"reason",
	"issued_at",
	"expires_at",
	"plan_digest",
}

destructive_approval_fields := {
	"kind",
	"address",
	"resource",
	"change",
	"ticket",
	"owner",
	"reason",
	"issued_at",
	"expires_at",
	"plan_digest",
}

default context := {}

context := object.get(data.policy_input, "mindclade", {}) if {
	is_object(data.policy_input)
}

profile := object.get(context, "profile", {})
runtime := object.get(context, "runtime", {})
approvals := object.get(context, "approvals", [])

deny contains policy_result(
	"POLICY_CONTEXT_INVALID",
	"policy-context",
	"A schema_version 1 profile with non-empty locations, residency, labels, classifications, data resource types, retention controls, and a computed plan digest is required.",
) if {
	not policy_context_valid
}

deny contains policy_result(
	"PLAN_JSON_INVALID",
	"terraform-plan",
	"Input must be Terraform plan JSON with a resource_changes array.",
) if {
	policy_context_valid
	not plan_shape_valid
}

deny contains policy_result(
	"APPROVAL_INVALID",
	sprintf("approval[%d]", [index]),
	"Approval is malformed, expired, unbound, wildcarded, or uses an unsupported kind.",
) if {
	policy_context_valid
	some index
	approval := approvals[index]
	not approval_valid(approval)
}

# IAM -------------------------------------------------------------------------

deny contains policy_result(
	"AUTHORITATIVE_IAM_FORBIDDEN",
	change.address,
	"IAM binding/policy resources are authoritative and can remove grants owned by another state; use additive *_iam_member resources.",
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_managed_change(change)
	authoritative_iam_type(change.type)
}

deny contains policy_result(
	"IAM_GRANT_UNRESOLVED",
	change.address,
	"IAM member/binding role and principals must be concrete, non-empty values with no unresolved after_unknown markers.",
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_managed_change(change)
	regex.match(`_iam_(member|binding)$`, change.type)
	not iam_grant_shape_known(change)
}

deny contains policy_result(
	"IAM_POLICY_UNPARSEABLE",
	change.address,
	"Authoritative IAM policy_data must be known JSON with non-empty bindings, concrete roles, and concrete non-empty member arrays.",
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_managed_change(change)
	endswith(change.type, "_iam_policy")
	not iam_policy_shape_known(change)
}

deny contains policy_result(
	"IAM_TARGET_UNRESOLVED",
	change.address,
	"IAM grants require a concrete provider resource target with no unresolved after_unknown marker.",
) if {
	evaluation_ready
	change := input.resource_changes[_]
	iam_grants(change)[_]
	not iam_target_known(change)
}

deny contains policy_result(
	"IAM_PUBLIC_PRINCIPAL",
	change.address,
	sprintf("Public principal %q is forbidden for role %q.", [grant.member, grant.role]),
) if {
	evaluation_ready
	change := input.resource_changes[_]
	grant := iam_grants(change)[_]
	is_public_principal(grant.member)
}

deny contains policy_result(
	"IAM_PRIMITIVE_ROLE",
	change.address,
	sprintf("Primitive role %q is forbidden for %q; use a least-privilege predefined or custom role.", [grant.role, grant.member]),
) if {
	evaluation_ready
	change := input.resource_changes[_]
	grant := iam_grants(change)[_]
	is_primitive_role(grant.role)
}

deny contains policy_result(
	"IAM_ADMIN_APPROVAL_REQUIRED",
	change.address,
	sprintf("Administrative grant %q to %q on %q requires an exact, unexpired approval bound to this plan digest.", [grant.role, grant.member, iam_target_display(change)]),
) if {
	evaluation_ready
	change := input.resource_changes[_]
	grant := iam_grants(change)[_]
	is_administrative_role(grant.role)
	not admin_approval_matches(change, grant.member, grant.role)
}

deny contains policy_result(
	"SERVICE_ACCOUNT_KEY_FORBIDDEN",
	change.address,
	"Terraform-managed service-account keys are forbidden; use Workload Identity Federation or another keyless identity.",
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_managed_change(change)
	change.type == "google_service_account_key"
}

# Destructive changes ---------------------------------------------------------

deny contains policy_result(
	"DESTRUCTIVE_APPROVAL_REQUIRED",
	change.address,
	sprintf("%s of %q requires an exact, unexpired approval bound to this plan digest.", [change_kind(change.change.actions), resource_identifier(change)]),
) if {
	evaluation_ready
	change := input.resource_changes[_]
	managed_change(change)
	destructive_actions(change.change.actions)
	not destructive_approval_matches(change)
}

# Data governance -------------------------------------------------------------

deny contains policy_result(
	"DATA_CLASSIFICATION_INVALID",
	change.address,
	sprintf("Label data-classification must select a configured policy class; got %q.", [classification(change)]),
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_data_change(change)
	not classification_configured(change)
}

deny contains policy_result(
	"REQUIRED_LABEL_MISSING",
	change.address,
	sprintf("Required label %q must have a non-empty value.", [label]),
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_data_change(change)
	label := profile.required_labels[_]
	not valid_exact_string(object.get(resource_labels(change), label, ""))
}

deny contains policy_result(
	"LOCATION_MISSING",
	change.address,
	"A concrete location or persistence region is required for residency enforcement.",
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_data_change(change)
	not has_resource_location(change)
}

deny contains policy_result(
	"LOCATION_NOT_APPROVED",
	change.address,
	sprintf("Location %q is not in the global approved-locations set.", [location]),
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_data_change(change)
	location := resource_locations(change)[_]
	not location_in(location, profile.approved_locations)
}

deny contains policy_result(
	"RESIDENCY_VIOLATION",
	change.address,
	sprintf("Location %q is not allowed for data classification %q.", [location, classification(change)]),
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_data_change(change)
	classification_configured(change)
	location := resource_locations(change)[_]
	class := profile.classifications[classification(change)]
	not location_in(location, class.allowed_locations)
}

deny contains policy_result(
	"CMEK_REQUIRED",
	change.address,
	sprintf("Data classification %q requires customer-managed encryption for %q.", [classification(change), change.type]),
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_data_change(change)
	classification_configured(change)
	class := profile.classifications[classification(change)]
	class.require_cmek
	not resource_has_cmek(change)
}

deny contains policy_result(
	"RETENTION_INSUFFICIENT",
	change.address,
	sprintf("Retention or recovery settings for %q do not meet the %q classification minimum.", [change.type, classification(change)]),
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_data_change(change)
	classification_configured(change)
	control := profile.classifications[classification(change)].retention[change.type]
	not retention_satisfied(change, control)
}

deny contains policy_result(
	"DELETION_PROTECTION_REQUIRED",
	change.address,
	sprintf("Provider-native deletion protection is not enabled for %q.", [change.type]),
) if {
	evaluation_ready
	change := input.resource_changes[_]
	active_data_change(change)
	not provider_deletion_protected(change)
}

# Context and plan validation -------------------------------------------------

evaluation_ready if {
	policy_context_valid
	plan_shape_valid
}

policy_context_valid if {
	profile_valid
	runtime_valid
	is_array(approvals)
}

profile_valid if {
	is_object(profile)
	object_keys_exact(profile, profile_fields)
	object.get(profile, "schema_version", 0) == 1
	valid_exact_string(object.get(profile, "profile_id", ""))

	approved_locations := object.get(profile, "approved_locations", null)
	is_array(approved_locations)
	count(approved_locations) > 0
	array_unique(approved_locations)
	every location in approved_locations {
		valid_location(location)
	}

	required_labels := object.get(profile, "required_labels", null)
	is_array(required_labels)
	count(required_labels) > 0
	array_unique(required_labels)
	"data-classification" in required_labels
	every label in required_labels {
		valid_exact_string(label)
	}

	data_resource_types := object.get(profile, "data_resource_types", null)
	is_array(data_resource_types)
	count(data_resource_types) > 0
	array_unique(data_resource_types)
	{resource_type | some resource_type in data_resource_types} == required_data_types

	classifications := object.get(profile, "classifications", null)
	is_object(classifications)
	count(classifications) > 0
	object.keys(classifications) == required_classifications
	every class_name in required_classifications {
		class := object.get(classifications, class_name, null)
		classification_control_valid(class)
	}
}

classification_control_valid(class) if {
	is_object(class)
	object_keys_exact(class, {"allowed_locations", "require_cmek", "retention"})
	is_boolean(object.get(class, "require_cmek", null))
	allowed_locations := object.get(class, "allowed_locations", null)
	is_array(allowed_locations)
	count(allowed_locations) > 0
	array_unique(allowed_locations)
	every location in allowed_locations {
		valid_location(location)
		location_in(location, profile.approved_locations)
	}
	retention := object.get(class, "retention", null)
	is_object(retention)
	object.keys(retention) == required_data_types
	every resource_type in required_data_types {
		control := object.get(retention, resource_type, null)
		retention_control_valid(resource_type, control)
	}
}

retention_control_valid(resource_type, control) if {
	resource_type in {
		"google_artifact_registry_repository",
		"google_pubsub_topic",
		"google_secret_manager_secret",
		"google_storage_bucket",
	}
	is_object(control)
	object_keys_exact(control, {"minimum_seconds"})
	minimum := object.get(control, "minimum_seconds", 0)
	positive_integer(minimum)
}

retention_control_valid("google_sql_database_instance", control) if {
	is_object(control)
	object_keys_exact(control, {"minimum_final_backup_days", "minimum_retained_backups"})
	minimum_backups := object.get(control, "minimum_retained_backups", 0)
	minimum_days := object.get(control, "minimum_final_backup_days", 0)
	positive_integer(minimum_backups)
	positive_integer(minimum_days)
}

retention_control_valid("google_redis_instance", control) if {
	is_object(control)
	object_keys_exact(control, {"maximum_snapshot_interval_seconds"})
	maximum_interval := object.get(control, "maximum_snapshot_interval_seconds", 0)
	positive_integer(maximum_interval)
}

runtime_valid if {
	is_object(runtime)
	digest := object.get(runtime, "plan_digest", "")
	is_string(digest)
	regex.match(`^[0-9a-f]{64}$`, digest)
	rfc3339_ns(object.get(runtime, "now", ""))
}

plan_shape_valid if {
	is_object(input)
	changes := object.get(input, "resource_changes", null)
	is_array(changes)
	every change in changes {
		plan_change_valid(change)
	}
}

plan_change_valid(change) if {
	is_object(change)
	valid_exact_string(object.get(change, "address", ""))
	valid_exact_string(object.get(change, "type", ""))
	object.get(change, "mode", "managed") in {"managed", "data"}
	planned_change := object.get(change, "change", null)
	is_object(planned_change)
	actions := object.get(planned_change, "actions", null)
	is_array(actions)
	count(actions) > 0
	every action in actions {
		action in {"no-op", "create", "read", "update", "delete"}
	}
}

managed_change(change) if {
	is_object(change)
	object.get(change, "mode", "managed") == "managed"
	is_object(object.get(change, "change", null))
	is_array(object.get(change.change, "actions", null))
}

active_managed_change(change) if {
	managed_change(change)
	object.get(change.change, "after", null) != null
}

active_data_change(change) if {
	active_managed_change(change)
	change.type in required_data_types
}

# Approval contract -----------------------------------------------------------

approval_valid(approval) if {
	admin_approval_valid(approval)
}

approval_valid(approval) if {
	destructive_approval_valid(approval)
}

approval_metadata_valid(approval, kind, fields) if {
	is_object(approval)
	object.get(approval, "kind", "") == kind
	every field in fields {
		valid_exact_string(object.get(approval, field, ""))
	}
	digest := object.get(approval, "plan_digest", "")
	regex.match(`^[0-9a-f]{64}$`, digest)
	digest == runtime.plan_digest
	issued_at := object.get(approval, "issued_at", "")
	expires_at := object.get(approval, "expires_at", "")
	approval_time_valid(issued_at, expires_at, runtime.now)
}

admin_approval_valid(approval) if {
	object_keys_exact(approval, admin_approval_fields)
	approval_metadata_valid(approval, "administrative_iam", [
		"address",
		"resource",
		"member",
		"role",
		"ticket",
		"owner",
		"reason",
		"issued_at",
		"expires_at",
		"plan_digest",
	])
	is_administrative_role(approval.role)
	not is_public_principal(approval.member)
}

destructive_approval_valid(approval) if {
	object_keys_exact(approval, destructive_approval_fields)
	approval_metadata_valid(approval, "destructive_change", [
		"address",
		"resource",
		"change",
		"ticket",
		"owner",
		"reason",
		"issued_at",
		"expires_at",
		"plan_digest",
	])
	approval.change in {"delete", "replace"}
}

admin_approval_matches(change, member, role) if {
	some approval in approvals
	admin_approval_valid(approval)
	iam_target_known(change)
	approval.address == change.address
	approval.resource == iam_target(change)
	approval.member == member
	approval.role == role
}

destructive_approval_matches(change) if {
	some approval in approvals
	destructive_approval_valid(approval)
	approval.address == change.address
	approval.resource == resource_identifier(change)
	approval.change == change_kind(change.change.actions)
}

approval_time_valid(issued_at, expires_at, now) if {
	issued_ns := rfc3339_ns(issued_at)
	expires_ns := rfc3339_ns(expires_at)
	now_ns := rfc3339_ns(now)
	issued_ns <= now_ns
	expires_ns > now_ns
	expires_ns - issued_ns <= approval_max_validity_ns
}

rfc3339_ns(value) := parsed if {
	is_string(value)
	regex.match(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?Z$`, value)
	parsed := time.parse_rfc3339_ns(value)
}

valid_exact_string(value) if {
	is_string(value)
	trim_space(value) != ""
	not contains(value, "*")
	not contains(value, "?")
	not contains(value, "[")
	not contains(value, "]")
}

known_nonempty_string(value) if {
	is_string(value)
	trim_space(value) != ""
}

authoritative_iam_type(resource_type) if {
	regex.match(`_iam_(binding|policy)$`, resource_type)
}

positive_integer(value) if {
	is_number(value)
	value > 0
	floor(value) == value
}

valid_location(value) if {
	valid_exact_string(value)
	lower(value) == value
}

array_unique(values) if {
	count(values) == count({value | some value in values})
}

object_keys_exact(value, expected) if {
	is_object(value)
	object.keys(value) == expected
}

object_or_empty(value) := value if {
	is_object(value)
}

object_or_empty(value) := {} if {
	not is_object(value)
}

first_object(values) := values[0] if {
	is_array(values)
	count(values) > 0
	is_object(values[0])
}

first_object(values) := {} if {
	not is_array(values)
}

first_object(values) := {} if {
	is_array(values)
	count(values) == 0
}

first_object(values) := {} if {
	is_array(values)
	count(values) > 0
	not is_object(values[0])
}

# IAM extraction --------------------------------------------------------------

iam_grant_shape_known(change) if {
	endswith(change.type, "_iam_member")
	after := change.change.after
	is_object(after)
	unknowns := object.get(change.change, "after_unknown", {})
	is_object(unknowns)
	iam_scalar_known(after, unknowns, "role")
	iam_scalar_known(after, unknowns, "member")
}

iam_policy_shape_known(change) if {
	endswith(change.type, "_iam_policy")
	after := change.change.after
	is_object(after)
	unknowns := object.get(change.change, "after_unknown", {})
	is_object(unknowns)
	iam_scalar_known(after, unknowns, "policy_data")
	policy := iam_policy_object(change)
	bindings := object.get(policy, "bindings", null)
	is_array(bindings)
	count(bindings) > 0
	every binding in bindings {
		iam_policy_binding_known(binding)
	}
}

iam_policy_binding_known(binding) if {
	is_object(binding)
	known_nonempty_string(object.get(binding, "role", null))
	members := object.get(binding, "members", null)
	is_array(members)
	count(members) > 0
	every member in members {
		known_nonempty_string(member)
	}
}

iam_grant_shape_known(change) if {
	endswith(change.type, "_iam_binding")
	after := change.change.after
	is_object(after)
	unknowns := object.get(change.change, "after_unknown", {})
	is_object(unknowns)
	iam_scalar_known(after, unknowns, "role")
	members := object.get(after, "members", null)
	is_array(members)
	count(members) > 0
	every member in members {
		known_nonempty_string(member)
	}
	iam_members_unknown_free(object.get(unknowns, "members", false))
}

iam_scalar_known(after, unknowns, field) if {
	known_nonempty_string(object.get(after, field, null))
	object.get(unknowns, field, false) == false
}

iam_members_unknown_free(marker) if {
	marker == false
}

iam_members_unknown_free(marker) if {
	is_array(marker)
	every element in marker {
		element == false
	}
}

iam_grants(change) := [{"member": member, "role": role}] if {
	active_managed_change(change)
	endswith(change.type, "_iam_member")
	iam_grant_shape_known(change)
	after := change.change.after
	role := object.get(after, "role", "")
	member := object.get(after, "member", "")
}

iam_grants(change) := grants if {
	active_managed_change(change)
	endswith(change.type, "_iam_binding")
	iam_grant_shape_known(change)
	after := change.change.after
	role := object.get(after, "role", "")
	members := object.get(after, "members", [])
	grants := [{"member": member, "role": role} |
		some member in members
	]
}

iam_grants(change) := grants if {
	active_managed_change(change)
	endswith(change.type, "_iam_policy")
	iam_policy_shape_known(change)
	policy := iam_policy_object(change)
	grants := [{"member": member, "role": role} |
		binding := policy.bindings[_]
		role := object.get(binding, "role", "")
		members := object.get(binding, "members", [])
		member := members[_]
	]
}

iam_policy_object(change) := policy if {
	raw := object.get(change.change.after, "policy_data", "")
	is_string(raw)
	policy := json.unmarshal(raw)
	is_object(policy)
}

is_public_principal(member) if {
	lower(member) in {"allusers", "allauthenticatedusers"}
}

is_primitive_role(role) if {
	lower(role) in {"roles/owner", "roles/editor", "roles/viewer"}
}

is_administrative_role(role) if {
	endswith(lower(role), "admin")
	not is_primitive_role(role)
}

iam_target(change) := target if {
	iam_target_known(change)
	field := iam_target_field(change)
	after := change.change.after
	target := sprintf("%v", [after[field]])
}

iam_target_display(change) := iam_target(change) if {
	iam_target_known(change)
}

iam_target_display(change) := "<unknown>" if {
	not iam_target_known(change)
}

iam_target_known(change) if {
	after := change.change.after
	is_object(after)
	unknowns := object.get(change.change, "after_unknown", {})
	is_object(unknowns)
	field := iam_target_field(change)
	iam_scalar_known(after, unknowns, field)
}

iam_target_field(change) := "bucket" if startswith(change.type, "google_storage_bucket_iam_")
iam_target_field(change) := "service_account_id" if startswith(change.type, "google_service_account_iam_")
iam_target_field(change) := "topic" if startswith(change.type, "google_pubsub_topic_iam_")
iam_target_field(change) := "subscription" if startswith(change.type, "google_pubsub_subscription_iam_")
iam_target_field(change) := "schema" if startswith(change.type, "google_pubsub_schema_iam_")
iam_target_field(change) := "secret_id" if startswith(change.type, "google_secret_manager_secret_iam_")
iam_target_field(change) := "repository" if startswith(change.type, "google_artifact_registry_repository_iam_")
iam_target_field(change) := "crypto_key_id" if startswith(change.type, "google_kms_crypto_key_iam_")
iam_target_field(change) := "key_ring_id" if startswith(change.type, "google_kms_key_ring_iam_")
iam_target_field(change) := "note" if startswith(change.type, "google_container_analysis_note_iam_")
iam_target_field(change) := "attestor" if startswith(change.type, "google_binary_authorization_attestor_iam_")
iam_target_field(change) := "web_backend_service" if startswith(change.type, "google_iap_web_backend_service_iam_")
iam_target_field(change) := "folder" if startswith(change.type, "google_folder_iam_")
iam_target_field(change) := "org_id" if startswith(change.type, "google_organization_iam_")
iam_target_field(change) := "project" if startswith(change.type, "google_project_iam_")

# Destructive change extraction ---------------------------------------------

destructive_actions(actions) if {
	actions == ["delete"]
}

destructive_actions(actions) if {
	{action | some action in actions} == {"create", "delete"}
}

change_kind(actions) := "delete" if {
	actions == ["delete"]
}

change_kind(actions) := "replace" if {
	{action | some action in actions} == {"create", "delete"}
}

resource_identifier(change) := sprintf("%v", [identifier]) if {
	before := object.get(change.change, "before", {})
	identifier := object.get(before, "id", object.get(before, "name", change.address))
}

# Labels, locations, encryption, retention, and deletion protection ----------

resource_labels(change) := object_or_empty(object.get(settings, "user_labels", {})) if {
	change.type == "google_sql_database_instance"
	settings := first_object(object.get(change.change.after, "settings", []))
}

resource_labels(change) := object_or_empty(object.get(change.change.after, "labels", {})) if {
	change.type != "google_sql_database_instance"
}

classification(change) := lower(value) if {
	value := object.get(resource_labels(change), "data-classification", "")
	is_string(value)
}

classification(change) := "" if {
	value := object.get(resource_labels(change), "data-classification", "")
	not is_string(value)
}

classification_configured(change) if {
	class := classification(change)
	class in required_classifications
	is_object(object.get(profile.classifications, class, null))
}

resource_locations(change) := [lower(location)] if {
	change.type in {"google_storage_bucket", "google_artifact_registry_repository"}
	location := object.get(change.change.after, "location", "")
	valid_exact_string(location)
}

resource_locations(change) := [lower(location)] if {
	change.type in {"google_sql_database_instance", "google_redis_instance"}
	location := object.get(change.change.after, "region", "")
	valid_exact_string(location)
}

resource_locations(change) := locations if {
	change.type == "google_pubsub_topic"
	policies := object.get(change.change.after, "message_storage_policy", [])
	locations := [lower(location) |
		policy := policies[_]
		configured := object.get(policy, "allowed_persistence_regions", [])
		location := configured[_]
		valid_exact_string(location)
	]
}

resource_locations(change) := locations if {
	change.type == "google_secret_manager_secret"
	replication := object.get(change.change.after, "replication", [])[0]
	automatic := object.get(replication, "auto", [])
	user_managed := object.get(replication, "user_managed", [])
	automatic_locations := ["global" |
		some _ in automatic
	]
	user_locations := [lower(location) |
		managed := user_managed[_]
		replicas := object.get(managed, "replicas", [])
		replica := replicas[_]
		location := object.get(replica, "location", "")
		valid_exact_string(location)
	]
	locations := array.concat(automatic_locations, user_locations)
}

has_resource_location(change) if {
	count(resource_locations(change)) > 0
}

location_in(location, configured) if {
	some candidate in configured
	lower(candidate) == lower(location)
}

resource_has_cmek(change) if {
	change.type == "google_storage_bucket"
	encryption := object.get(change.change.after, "encryption", [])[_]
	valid_exact_string(object.get(encryption, "default_kms_key_name", ""))
}

resource_has_cmek(change) if {
	change.type == "google_secret_manager_secret"
	secret_replication_has_cmek(change.change.after)
}

resource_has_cmek(change) if {
	change.type == "google_pubsub_topic"
	valid_exact_string(object.get(change.change.after, "kms_key_name", ""))
}

resource_has_cmek(change) if {
	change.type == "google_artifact_registry_repository"
	valid_exact_string(object.get(change.change.after, "kms_key_name", ""))
}

resource_has_cmek(change) if {
	change.type == "google_sql_database_instance"
	valid_exact_string(object.get(change.change.after, "encryption_key_name", ""))
}

resource_has_cmek(change) if {
	change.type == "google_redis_instance"
	valid_exact_string(object.get(change.change.after, "customer_managed_key", ""))
}

secret_replication_has_cmek(after) if {
	replication := object.get(after, "replication", [])[0]
	automatic := object.get(replication, "auto", [])
	count(automatic) > 0
	every auto in automatic {
		encryption := object.get(auto, "customer_managed_encryption", [])
		count(encryption) > 0
		every key in encryption {
			valid_exact_string(object.get(key, "kms_key_name", ""))
		}
	}
}

secret_replication_has_cmek(after) if {
	replication := object.get(after, "replication", [])[0]
	user_managed := object.get(replication, "user_managed", [])
	count(user_managed) > 0
	every managed in user_managed {
		replicas := object.get(managed, "replicas", [])
		count(replicas) > 0
		every replica in replicas {
			encryption := object.get(replica, "customer_managed_encryption", [])
			count(encryption) > 0
			every key in encryption {
				valid_exact_string(object.get(key, "kms_key_name", ""))
			}
		}
	}
}

retention_satisfied(change, control) if {
	change.type == "google_storage_bucket"
	policies := object.get(change.change.after, "retention_policy", [])
	count(policies) > 0
	every policy in policies {
		seconds := duration_seconds(object.get(policy, "retention_period", 0))
		seconds >= control.minimum_seconds
	}
}

retention_satisfied(change, control) if {
	change.type == "google_secret_manager_secret"
	seconds := duration_seconds(object.get(change.change.after, "version_destroy_ttl", ""))
	seconds >= control.minimum_seconds
}

retention_satisfied(change, control) if {
	change.type == "google_pubsub_topic"
	seconds := duration_seconds(object.get(change.change.after, "message_retention_duration", ""))
	seconds >= control.minimum_seconds
}

retention_satisfied(change, control) if {
	change.type == "google_artifact_registry_repository"
	policies := object.get(change.change.after, "cleanup_policies", [])
	delete_policies := [policy |
		some policy in policies
		upper(object.get(policy, "action", "")) == "DELETE"
	]
	count(delete_policies) > 0
	every policy in delete_policies {
		condition := object.get(policy, "condition", [])[0]
		seconds := duration_seconds(object.get(condition, "older_than", ""))
		seconds >= control.minimum_seconds
	}
}

retention_satisfied(change, control) if {
	change.type == "google_sql_database_instance"
	settings := object.get(change.change.after, "settings", [])[0]
	backup := object.get(settings, "backup_configuration", [])[0]
	object.get(backup, "enabled", false)
	retention := object.get(backup, "backup_retention_settings", [])[0]
	object.get(retention, "retained_backups", 0) >= control.minimum_retained_backups
	final_backup := object.get(settings, "final_backup_config", [])[0]
	object.get(final_backup, "enabled", false)
	object.get(final_backup, "retention_days", 0) >= control.minimum_final_backup_days
}

retention_satisfied(change, control) if {
	change.type == "google_redis_instance"
	persistence := object.get(change.change.after, "persistence_config", [])[0]
	object.get(persistence, "persistence_mode", "") == "RDB"
	snapshot_period_seconds(object.get(persistence, "rdb_snapshot_period", "")) <= control.maximum_snapshot_interval_seconds
}

duration_seconds(value) := value if {
	is_number(value)
}

duration_seconds(value) := to_number(trim_suffix(value, "s")) if {
	is_string(value)
	regex.match(`^[0-9]+s$`, value)
}

duration_seconds(value) := to_number(trim_suffix(value, "m")) * 60 if {
	is_string(value)
	regex.match(`^[0-9]+m$`, value)
}

duration_seconds(value) := to_number(trim_suffix(value, "h")) * 3600 if {
	is_string(value)
	regex.match(`^[0-9]+h$`, value)
}

duration_seconds(value) := to_number(trim_suffix(value, "d")) * 86400 if {
	is_string(value)
	regex.match(`^[0-9]+d$`, value)
}

snapshot_period_seconds("ONE_HOUR") := 3600
snapshot_period_seconds("SIX_HOURS") := 21600
snapshot_period_seconds("TWELVE_HOURS") := 43200
snapshot_period_seconds("TWENTY_FOUR_HOURS") := 86400

provider_deletion_protected(change) if {
	change.type == "google_storage_bucket"
	after := change.change.after
	object.get(after, "deletion_policy", "") == "PREVENT"
	object.get(after, "force_destroy", true) == false
}

provider_deletion_protected(change) if {
	change.type == "google_secret_manager_secret"
	after := change.change.after
	object.get(after, "deletion_policy", "") == "PREVENT"
	object.get(after, "deletion_protection", false)
}

provider_deletion_protected(change) if {
	change.type == "google_artifact_registry_repository"
	object.get(change.change.after, "deletion_policy", "") == "PREVENT"
}

provider_deletion_protected(change) if {
	change.type == "google_sql_database_instance"
	after := change.change.after
	object.get(after, "deletion_policy", "") == "PREVENT"
	object.get(after, "deletion_protection", false)
	settings := object.get(after, "settings", [])[0]
	object.get(settings, "deletion_protection_enabled", false)
}

provider_deletion_protected(change) if {
	change.type == "google_redis_instance"
	after := change.change.after
	object.get(after, "deletion_policy", "") == "PREVENT"
	object.get(after, "deletion_protection", false)
}

# Pub/Sub topics do not expose provider-native deletion protection. Deletion and
# replacement are still governed by the plan-digest approval rule above.
provider_deletion_protected(change) if {
	change.type == "google_pubsub_topic"
}

policy_result(code, address, message) := {
	"msg": sprintf("%s: %s [%s]", [code, message, address]),
	"metadata": {
		"address": address,
		"code": code,
	},
}
