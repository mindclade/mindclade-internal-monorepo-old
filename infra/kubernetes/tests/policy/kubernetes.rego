# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

package main

import rego.v1

controller_kinds := {
	"DaemonSet",
	"Deployment",
	"Job",
	"ReplicaSet",
	"ReplicationController",
	"StatefulSet",
}

pod_specs contains pod_spec if {
	input.kind == "Pod"
	pod_spec := input.spec
}

pod_specs contains pod_spec if {
	input.kind in controller_kinds
	pod_spec := input.spec.template.spec
}

pod_specs contains pod_spec if {
	input.kind == "CronJob"
	pod_spec := input.spec.jobTemplate.spec.template.spec
}

pod_specs contains pod_spec if {
	input.kind == "JobSet"
	some replicated_job in input.spec.replicatedJobs
	pod_spec := replicated_job.template.spec.template.spec
}

pod_containers contains container if {
	some pod_spec in pod_specs
	some container in object.get(pod_spec, "containers", [])
}

pod_containers contains container if {
	some pod_spec in pod_specs
	some container in object.get(pod_spec, "initContainers", [])
}

pod_spec_containers(pod_spec) := containers if {
	containers := array.concat(
		object.get(pod_spec, "containers", []),
		object.get(pod_spec, "initContainers", []),
	)
}

resource_name := object.get(object.get(input, "metadata", {}), "name", "<unnamed>")

operator_service_accounts := {
	"jobset-system/jobset-controller",
	"kueue-system/kueue-controller-manager",
}

operator_service_account if {
	namespace := object.get(object.get(input, "metadata", {}), "namespace", "")
	identity := sprintf("%s/%s", [namespace, resource_name])
	identity in operator_service_accounts
}

operator_pod_service_account(pod_spec) if {
	namespace := object.get(object.get(input, "metadata", {}), "namespace", "")
	service_account := object.get(pod_spec, "serviceAccountName", "")
	identity := sprintf("%s/%s", [namespace, service_account])
	identity in operator_service_accounts
}

activation_gated if {
	input.kind in controller_kinds
	object.get(input.spec, "replicas", 1) == 0
}

activation_gated if {
	input.kind == "Job"
	object.get(input.spec, "suspend", false) == true
}

activation_gated if {
	input.kind == "CronJob"
	object.get(input.spec, "suspend", false) == true
}

activation_gated if {
	input.kind == "JobSet"
	object.get(input.spec, "suspend", false) == true
}

deny contains msg if {
	input.kind == "Secret"
	msg := sprintf("%s: Kubernetes Secret manifests are forbidden; reference an external secret source", [resource_name])
}

deny contains msg if {
	input.kind == "ServiceAccount"
	object.get(input, "automountServiceAccountToken", true) != false
	not operator_service_account
	msg := sprintf("ServiceAccount/%s must set automountServiceAccountToken: false", [resource_name])
}

deny contains msg if {
	input.kind == "Namespace"
	labels := object.get(object.get(input, "metadata", {}), "labels", {})
	object.get(labels, "pod-security.kubernetes.io/enforce", "") != "restricted"
	msg := sprintf("Namespace/%s must enforce the restricted Pod Security Standard", [resource_name])
}

deny contains msg if {
	input.kind == "Service"
	service_type := object.get(input.spec, "type", "ClusterIP")
	service_type in {"ExternalName", "LoadBalancer", "NodePort"}
	msg := sprintf("Service/%s uses forbidden externally reachable type %s", [resource_name, service_type])
}

deny contains msg if {
	input.kind == "Service"
	external_ips := object.get(input.spec, "externalIPs", [])
	count(external_ips) != 0
	msg := sprintf("Service/%s must not set externalIPs", [resource_name])
}

deny contains msg if {
	some pod_spec in pod_specs
	object.get(pod_spec, "hostNetwork", false)
	msg := sprintf("%s/%s must not use hostNetwork", [input.kind, resource_name])
}

deny contains msg if {
	some pod_spec in pod_specs
	object.get(pod_spec, "hostPID", false)
	msg := sprintf("%s/%s must not use hostPID", [input.kind, resource_name])
}

deny contains msg if {
	some pod_spec in pod_specs
	object.get(pod_spec, "hostIPC", false)
	msg := sprintf("%s/%s must not use hostIPC", [input.kind, resource_name])
}

deny contains msg if {
	some pod_spec in pod_specs
	some volume in object.get(pod_spec, "volumes", [])
	object.get(volume, "hostPath", null) != null
	msg := sprintf("%s/%s must not mount hostPath volume %s", [input.kind, resource_name, volume.name])
}

deny contains msg if {
	some pod_spec in pod_specs
	count(object.get(pod_spec, "ephemeralContainers", [])) != 0
	msg := sprintf("%s/%s must not define ephemeral containers", [input.kind, resource_name])
}

deny contains msg if {
	some pod_spec in pod_specs
	service_account := object.get(pod_spec, "serviceAccountName", "")
	service_account in {"", "default"}
	msg := sprintf("%s/%s must use a named non-default ServiceAccount", [input.kind, resource_name])
}

deny contains msg if {
	some pod_spec in pod_specs
	object.get(pod_spec, "automountServiceAccountToken", true) != false
	not operator_pod_service_account(pod_spec)
	msg := sprintf("%s/%s must disable automatic ServiceAccount token mounting", [input.kind, resource_name])
}

deny contains msg if {
	some pod_spec in pod_specs
	security_context := object.get(pod_spec, "securityContext", {})
	object.get(security_context, "runAsNonRoot", false) != true
	msg := sprintf("%s/%s must set pod securityContext.runAsNonRoot: true", [input.kind, resource_name])
}

deny contains msg if {
	some pod_spec in pod_specs
	security_context := object.get(pod_spec, "securityContext", {})
	seccomp_profile := object.get(security_context, "seccompProfile", {})
	object.get(seccomp_profile, "type", "") != "RuntimeDefault"
	msg := sprintf("%s/%s must use the RuntimeDefault seccomp profile", [input.kind, resource_name])
}

deny contains msg if {
	some container in pod_containers
	not regex.match("@sha256:[0-9a-f]{64}$", container.image)
	msg := sprintf("%s/%s container %s must use an immutable sha256 digest", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some container in pod_containers
	regex.match("@sha256:0{64}$", container.image)
	not activation_gated
	msg := sprintf("%s/%s container %s uses the activation-only zero digest while the workload is enabled", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some container in pod_containers
	security_context := object.get(container, "securityContext", {})
	object.get(security_context, "privileged", false)
	msg := sprintf("%s/%s container %s must not be privileged", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some container in pod_containers
	security_context := object.get(container, "securityContext", {})
	object.get(security_context, "allowPrivilegeEscalation", true) != false
	msg := sprintf("%s/%s container %s must disable privilege escalation", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some container in pod_containers
	security_context := object.get(container, "securityContext", {})
	object.get(security_context, "readOnlyRootFilesystem", false) != true
	msg := sprintf("%s/%s container %s must use a read-only root filesystem", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some container in pod_containers
	security_context := object.get(container, "securityContext", {})
	capabilities := object.get(security_context, "capabilities", {})
	dropped := object.get(capabilities, "drop", [])
	not "ALL" in dropped
	msg := sprintf("%s/%s container %s must drop the ALL capability set", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some container in pod_containers
	some container_port in object.get(container, "ports", [])
	object.get(container_port, "hostPort", 0) != 0
	msg := sprintf("%s/%s container %s must not expose a hostPort", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some container in pod_containers
	requests := object.get(object.get(container, "resources", {}), "requests", {})
	object.get(requests, "cpu", "") == ""
	msg := sprintf("%s/%s container %s must request CPU", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some container in pod_containers
	requests := object.get(object.get(container, "resources", {}), "requests", {})
	object.get(requests, "memory", "") == ""
	msg := sprintf("%s/%s container %s must request memory", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some container in pod_containers
	limits := object.get(object.get(container, "resources", {}), "limits", {})
	object.get(limits, "cpu", "") == ""
	msg := sprintf("%s/%s container %s must limit CPU", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some container in pod_containers
	limits := object.get(object.get(container, "resources", {}), "limits", {})
	object.get(limits, "memory", "") == ""
	msg := sprintf("%s/%s container %s must limit memory", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some pod_spec in pod_specs
	not operator_pod_service_account(pod_spec)
	some container in pod_spec_containers(pod_spec)
	requests := object.get(object.get(container, "resources", {}), "requests", {})
	object.get(requests, "ephemeral-storage", "") == ""
	msg := sprintf("%s/%s container %s must request ephemeral storage", [input.kind, resource_name, container.name])
}

deny contains msg if {
	some pod_spec in pod_specs
	not operator_pod_service_account(pod_spec)
	some container in pod_spec_containers(pod_spec)
	limits := object.get(object.get(container, "resources", {}), "limits", {})
	object.get(limits, "ephemeral-storage", "") == ""
	msg := sprintf("%s/%s container %s must limit ephemeral storage", [input.kind, resource_name, container.name])
}
