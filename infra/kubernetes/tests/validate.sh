#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail
IFS=$'\n\t'

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
kubernetes_root="$(cd -- "${script_dir}/.." && pwd)"
validation_config="${script_dir}/validation-config.yaml"
policy_dir="${script_dir}/policy"

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

note() {
  printf '\n==> %s\n' "$*"
}

require_tool() {
  local tool_name="$1"
  local install_hint="$2"
  command -v "${tool_name}" >/dev/null 2>&1 ||
    fail "required validation tool '${tool_name}' is missing; ${install_hint}"
}

require_tool conftest "enter the repository's pinned Kubernetes validation environment"
require_tool diff "install POSIX diff utilities"
require_tool find "install POSIX find utilities"
require_tool grep "install POSIX grep utilities"
require_tool kubeconform "enter the repository's pinned Kubernetes validation environment"
require_tool kustomize "enter the repository's pinned Kubernetes validation environment"
require_tool mktemp "install POSIX temporary-file utilities"
require_tool sed "install POSIX sed utilities"
require_tool sort "install POSIX sort utilities"
require_tool yq "enter the repository's pinned Kubernetes validation environment"

[[ -f "${validation_config}" ]] || fail "validation configuration is missing: ${validation_config}"
[[ -d "${policy_dir}" ]] || fail "Conftest policy directory is missing: ${policy_dir}"

# The validator uses mikefarah/yq syntax. Python yq accepts many of these expressions but gives
# different exit semantics, which can turn an empty selection into a false success.
yq_version="$(yq --version 2>&1)"
[[ "${yq_version}" == *"mikefarah/yq"* ]] ||
  fail "unsupported yq implementation (${yq_version}); mikefarah/yq v4 is required"

kubernetes_version="$(yq eval -r '.spec.kubernetesVersion' "${validation_config}")"
[[ "${kubernetes_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
  fail "spec.kubernetesVersion must be an explicit major.minor.patch value"

validation_tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/mindclade-kubernetes-validation.XXXXXX")"
cleanup() {
  # The target is an explicit directory returned by mktemp above, never a caller-supplied path.
  rm -rf -- "${validation_tmp_dir}"
}
trap cleanup EXIT

actual_roots="${validation_tmp_dir}/actual-roots.txt"
expected_roots="${validation_tmp_dir}/expected-roots.txt"
allowed_custom_gvks="${validation_tmp_dir}/allowed-custom-gvks.txt"
all_rendered="${validation_tmp_dir}/all-rendered.yaml"
core_rendered="${validation_tmp_dir}/core-rendered.yaml"
: >"${all_rendered}"

note "checking the declared Kustomize inventory"
while IFS= read -r kustomization_file; do
  relative_file="${kustomization_file#"${kubernetes_root}/"}"
  printf '%s\n' "${relative_file%/kustomization.yaml}"
done < <(find "${kubernetes_root}" -type f -name kustomization.yaml -print | LC_ALL=C sort) >"${actual_roots}"

{
  yq eval -r '.spec.requiredNonEmptyRoots[]' "${validation_config}"
  yq eval -r '.spec.activationGatedEmptyRoots[]' "${validation_config}"
} | LC_ALL=C sort -u >"${expected_roots}"

if ! diff -u "${expected_roots}" "${actual_roots}"; then
  fail "Kustomize inventory drifted; classify every root as required-non-empty or activation-gated"
fi

yq eval -r '.spec.allowedCustomGVKs[]' "${validation_config}" | LC_ALL=C sort -u \
  >"${allowed_custom_gvks}"

check_local_resources() {
  local root_name="$1"
  local kustomization_file="${kubernetes_root}/${root_name}/kustomization.yaml"
  local remote_reference_count

  remote_reference_count="$(
    yq eval '[.resources[]? | select(test("^(https?://|git::|git@|github\\.com/)|[?&]ref="))] | length' \
      "${kustomization_file}"
  )"
  [[ "${remote_reference_count}" == "0" ]] ||
    fail "${root_name}: remote Kustomize resources are forbidden; vendor and content-lock them"
}

render_root() {
  local root_name="$1"
  local empty_mode="$2"
  local root_dir="${kubernetes_root}/${root_name}"
  local output_name="${root_name//\//__}"
  local output_file="${validation_tmp_dir}/${output_name}.yaml"
  local resource_count

  [[ -d "${root_dir}" ]] || fail "declared Kustomize root does not exist: ${root_name}"
  [[ -f "${root_dir}/kustomization.yaml" ]] ||
    fail "declared Kustomize root has no kustomization.yaml: ${root_name}"
  [[ "$(yq eval -r '.kind // ""' "${root_dir}/kustomization.yaml")" == "Kustomization" ]] ||
    fail "${root_name}: kustomization.yaml is not a Kubernetes Kustomization"

  check_local_resources "${root_name}"
  if ! kustomize build --load-restrictor LoadRestrictionsRootOnly "${root_dir}" >"${output_file}"; then
    fail "${root_name}: kustomize build failed"
  fi

  resource_count="$(
    yq eval-all '[.] | flatten | map(select(.kind != null and .apiVersion != null)) | length' \
      "${output_file}"
  )"

  if [[ "${resource_count}" == "0" ]]; then
    if [[ "${empty_mode}" != "activation-gated" ]]; then
      fail "${root_name}: required module rendered zero Kubernetes resources"
    fi
    [[ "$(yq eval -o=json -I=0 '.resources' "${root_dir}/kustomization.yaml")" == "[]" ]] ||
      fail "${root_name}: an activation-gated empty module must declare resources: [] explicitly"
    printf 'ACTIVATION-GATED %-65s 0 resources\n' "${root_name}"
    return 0
  fi

  printf -- '---\n' >>"${all_rendered}"
  sed -e '$a\' "${output_file}" >>"${all_rendered}"
  printf 'RENDERED          %-65s %s resources\n' "${root_name}" "${resource_count}"
}

while IFS= read -r root_name; do
  render_root "${root_name}" required
done < <(yq eval -r '.spec.requiredNonEmptyRoots[]' "${validation_config}")

while IFS= read -r root_name; do
  render_root "${root_name}" activation-gated
done < <(yq eval -r '.spec.activationGatedEmptyRoots[]' "${validation_config}")

note "linting and rendering Helm charts when present"
chart_count=0
while IFS= read -r chart_file; do
  [[ -n "${chart_file}" ]] || continue
  if [[ "${chart_count}" == "0" ]]; then
    require_tool helm "enter the repository's pinned Kubernetes validation environment"
  fi
  chart_count=$((chart_count + 1))
  chart_dir="${chart_file%/Chart.yaml}"
  chart_name="${chart_dir#"${kubernetes_root}/"}"
  chart_output="${validation_tmp_dir}/helm-${chart_count}.yaml"
  helm lint --strict "${chart_dir}"
  helm template "mindclade-validation-${chart_count}" "${chart_dir}" --include-crds >"${chart_output}"
  chart_resource_count="$(
    yq eval-all '[.] | flatten | map(select(.kind != null and .apiVersion != null)) | length' \
      "${chart_output}"
  )"
  [[ "${chart_resource_count}" != "0" ]] || fail "${chart_name}: Helm chart rendered zero resources"
  printf -- '---\n' >>"${all_rendered}"
  sed -e '$a\' "${chart_output}" >>"${all_rendered}"
  printf 'HELM              %-65s %s resources\n' "${chart_name}" "${chart_resource_count}"
done < <(find "${kubernetes_root}" -type f -name Chart.yaml -print | LC_ALL=C sort)
[[ "${chart_count}" != "0" ]] || printf 'No Helm charts declared; Kustomize remains the source format.\n'

note "checking custom-resource kinds against the explicit allowlist"
while IFS=$'\t' read -r api_version resource_kind; do
  [[ -n "${api_version}" && -n "${resource_kind}" ]] || continue
  case "${api_version}" in
    v1 | apps/* | batch/* | autoscaling/* | policy/* | networking.k8s.io/* | \
      rbac.authorization.k8s.io/* | scheduling.k8s.io/* | storage.k8s.io/* | \
      coordination.k8s.io/* | admissionregistration.k8s.io/* | apiextensions.k8s.io/* | \
      authentication.k8s.io/* | authorization.k8s.io/* | certificates.k8s.io/* | \
      discovery.k8s.io/* | events.k8s.io/* | flowcontrol.apiserver.k8s.io/* | \
      node.k8s.io/* | resource.k8s.io/*)
      ;;
    *)
      custom_gvk="${api_version}/${resource_kind}"
      grep -Fqx -- "${custom_gvk}" "${allowed_custom_gvks}" ||
        fail "unreviewed custom GVK in rendered output: ${custom_gvk}"
      ;;
  esac
done < <(
  yq eval-all -r 'select(.kind != null and .apiVersion != null) | [.apiVersion, .kind] | @tsv' \
    "${all_rendered}" | LC_ALL=C sort -u
)

note "validating built-in Kubernetes resources against ${kubernetes_version} schemas"
yq eval-all '
  select(
    .kind != null and .apiVersion != null and
    (
      .apiVersion == "v1" or
      (.apiVersion | test("^(apps|batch|autoscaling|policy|networking.k8s.io|rbac.authorization.k8s.io|scheduling.k8s.io|storage.k8s.io|coordination.k8s.io|admissionregistration.k8s.io|apiextensions.k8s.io|authentication.k8s.io|authorization.k8s.io|certificates.k8s.io|discovery.k8s.io|events.k8s.io|flowcontrol.apiserver.k8s.io|node.k8s.io|resource.k8s.io)/"))
    )
  )
' "${all_rendered}" >"${core_rendered}"

core_resource_count="$(
  yq eval-all '[.] | flatten | map(select(.kind != null and .apiVersion != null)) | length' \
    "${core_rendered}"
)"
[[ "${core_resource_count}" != "0" ]] || fail "rendered inventory contains no built-in resources"
kubeconform \
  -exit-on-error \
  -kubernetes-version "${kubernetes_version}" \
  -strict \
  -summary \
  "${core_rendered}"

note "self-testing the fail-closed Conftest policy"
conftest test \
  --no-color \
  --policy "${policy_dir}" \
  --rego-version v1 \
  --strict \
  "${script_dir}/policy-pass-test.yaml" >/dev/null

negative_policy_output="${validation_tmp_dir}/negative-policy-output.txt"
if conftest test \
  --no-color \
  --policy "${policy_dir}" \
  --rego-version v1 \
  --strict \
  "${script_dir}/policy-test.yaml" >"${negative_policy_output}" 2>&1; then
  fail "negative policy fixture unexpectedly passed; the policy gate is not fail-closed"
fi
grep -Fq 'must use an immutable sha256 digest' "${negative_policy_output}" ||
  fail "negative policy fixture failed for an unexpected reason"

note "applying workload and security policy to deployable roots"
while IFS= read -r root_name; do
  output_name="${root_name//\//__}"
  output_file="${validation_tmp_dir}/${output_name}.yaml"
  [[ -s "${output_file}" ]] || fail "policy root did not produce a render: ${root_name}"

  conftest test \
    --no-color \
    --policy "${policy_dir}" \
    --rego-version v1 \
    --strict \
    "${output_file}"

  workload_count="$(
    yq eval-all '[.] | flatten | map(select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "DaemonSet" or .kind == "Job" or .kind == "CronJob" or .kind == "Pod" or .kind == "JobSet")) | length' \
      "${output_file}"
  )"
  if [[ "${workload_count}" != "0" ]]; then
    default_deny_count="$(
      yq eval-all '[.] | flatten | map(select(
        .kind == "NetworkPolicy" and
        .spec.podSelector == {} and
        (.spec.policyTypes | index("Ingress")) != null and
        (.spec.policyTypes | index("Egress")) != null
      )) | length' "${output_file}"
    )"
    [[ "${default_deny_count}" != "0" ]] ||
      fail "${root_name}: workloads require an ingress-and-egress default-deny NetworkPolicy"
  fi

  duplicate_identity_count="$(
    yq eval-all '[.] | flatten |
      map(select(.kind != null and .metadata.name != null)) |
      group_by(.apiVersion + "/" + .kind + "/" + (.metadata.namespace // "_cluster") + "/" + .metadata.name) |
      map(select(length > 1)) | length' "${output_file}"
  )"
  [[ "${duplicate_identity_count}" == "0" ]] ||
    fail "${root_name}: rendered output contains duplicate resource identities"

  printf 'POLICY            %s\n' "${root_name}"
done < <(yq eval -r '.spec.policyRoots[]' "${validation_config}")

note "Kubernetes validation passed"
printf 'Validated %s built-in rendered resources, %s Helm chart(s), and every declared Kustomize root.\n' \
  "${core_resource_count}" "${chart_count}"
