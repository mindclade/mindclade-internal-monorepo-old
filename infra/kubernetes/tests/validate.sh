#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail
IFS=$'\n\t'

entrypoint_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
if [[ -n "${TEST_SRCDIR:-}" && -n "${TEST_WORKSPACE:-}" &&
  -d "${TEST_SRCDIR}/${TEST_WORKSPACE}/infra/kubernetes/tests" ]]; then
  kubernetes_root="${TEST_SRCDIR}/${TEST_WORKSPACE}/infra/kubernetes"
  script_dir="${kubernetes_root}/tests"
else
  script_dir="${entrypoint_dir}"
  kubernetes_root="$(cd -- "${script_dir}/.." && pwd)"
fi
validation_config="${script_dir}/validation-config.yaml"
policy_dir="${script_dir}/policy"
version_lock="${kubernetes_root}/versions.env"

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

note() {
  printf '\n==> %s\n' "$*"
}

append_rendered_yaml() {
  local source_file="$1"

  printf -- '---\n' >>"${all_rendered}"
  # This sed append command deliberately ends in a backslash; it ensures a final newline without
  # relying on GNU-only flags. ShellCheck otherwise mistakes it for shell quote escaping.
  # shellcheck disable=SC1003
  sed -e '$a\' "${source_file}" >>"${all_rendered}"
}

require_tool() {
  local tool_name="$1"
  local install_hint="$2"
  command -v "${tool_name}" >/dev/null 2>&1 ||
    fail "required validation tool '${tool_name}' is missing; ${install_hint}"
}

version_value() {
  local variable_name="$1"
  local match_count

  [[ "${variable_name}" =~ ^[A-Z][A-Z0-9_]+$ ]] ||
    fail "invalid versions.env variable name in validation policy: ${variable_name}"
  match_count="$(grep -c "^${variable_name}=" "${version_lock}" || true)"
  [[ "${match_count}" == "1" ]] ||
    fail "versions.env must define ${variable_name} exactly once"
  sed -n "s/^${variable_name}=//p" "${version_lock}"
}

sha256_file() {
  local source_file="$1"
  local digest_output

  if command -v sha256sum >/dev/null 2>&1; then
    digest_output="$(sha256sum "${source_file}")"
  elif command -v shasum >/dev/null 2>&1; then
    digest_output="$(shasum -a 256 "${source_file}")"
  else
    fail "required SHA-256 tool is missing; install sha256sum or shasum"
  fi
  printf '%s\n' "${digest_output%% *}"
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
[[ -f "${version_lock}" ]] || fail "Kubernetes version lock is missing: ${version_lock}"

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

# Kustomize deliberately refuses symlinked kustomization files. Bazel runfiles are symlink trees,
# so a local Bazel test dereferences its declared data into the test-owned temporary directory.
# The no-sandbox/local tags keep the Nix-provided host tools available; this copy keeps source
# discovery identical between direct and Bazel execution without reading undeclared workspace data.
if [[ -n "${TEST_SRCDIR:-}" && -n "${TEST_WORKSPACE:-}" ]]; then
  require_tool cp "install POSIX file utilities"
  staged_kubernetes_root="${validation_tmp_dir}/kubernetes"
  cp -RL "${kubernetes_root}" "${staged_kubernetes_root}"
  kubernetes_root="${staged_kubernetes_root}"
fi

actual_roots="${validation_tmp_dir}/actual-roots.txt"
expected_roots="${validation_tmp_dir}/expected-roots.txt"
actual_charts="${validation_tmp_dir}/actual-charts.txt"
expected_charts="${validation_tmp_dir}/expected-charts.txt"
allowed_custom_gvks="${validation_tmp_dir}/allowed-custom-gvks.txt"
cluster_scoped_gvks="${validation_tmp_dir}/cluster-scoped-gvks.txt"
policy_roots="${validation_tmp_dir}/policy-roots.txt"
network_policy_roots="${validation_tmp_dir}/network-policy-roots.txt"
helm_policy_outputs="${validation_tmp_dir}/helm-policy-outputs.txt"
all_rendered="${validation_tmp_dir}/all-rendered.yaml"
core_rendered="${validation_tmp_dir}/core-rendered.yaml"
crd_rendered="${validation_tmp_dir}/crd-rendered.yaml"
chart_custom_rendered="${validation_tmp_dir}/chart-custom-rendered.yaml"
custom_schema_dir="${validation_tmp_dir}/custom-schemas"
: >"${all_rendered}"
: >"${helm_policy_outputs}"
: >"${chart_custom_rendered}"

note "checking the declared Kustomize inventory"
while IFS= read -r kustomization_file; do
  relative_file="${kustomization_file#"${kubernetes_root}/"}"
  printf '%s\n' "${relative_file%/kustomization.yaml}"
done < <(
  find "${kubernetes_root}" \( -type f -o -type l \) -name kustomization.yaml -print |
    LC_ALL=C sort
) >"${actual_roots}"

{
  yq eval -r '.spec.requiredNonEmptyRoots[]' "${validation_config}"
  yq eval -r '.spec.activationGatedEmptyRoots[]' "${validation_config}"
} | LC_ALL=C sort -u >"${expected_roots}"

if ! diff -u "${expected_roots}" "${actual_roots}"; then
  fail "Kustomize inventory drifted; classify every root as required-non-empty or activation-gated"
fi

yq eval -r '.spec.allowedCustomGVKs[]' "${validation_config}" | LC_ALL=C sort -u \
  >"${allowed_custom_gvks}"
yq eval -r '.spec.clusterScopedGVKs[]' "${validation_config}" | LC_ALL=C sort -u \
  >"${cluster_scoped_gvks}"
yq eval -r '.spec.policyRoots[]' "${validation_config}" | LC_ALL=C sort -u >"${policy_roots}"
yq eval -r '.spec.networkPolicyRoots[]' "${validation_config}" | LC_ALL=C sort -u \
  >"${network_policy_roots}"
while IFS= read -r root_name; do
  grep -Fqx -- "${root_name}" "${policy_roots}" ||
    fail "network-policy root is not also a policy root: ${root_name}"
done <"${network_policy_roots}"

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

  append_rendered_yaml "${output_file}"
  printf 'RENDERED          %-65s %s resources\n' "${root_name}" "${resource_count}"
}

while IFS= read -r root_name; do
  render_root "${root_name}" required
done < <(yq eval -r '.spec.requiredNonEmptyRoots[]' "${validation_config}")

while IFS= read -r root_name; do
  render_root "${root_name}" activation-gated
done < <(yq eval -r '.spec.activationGatedEmptyRoots[]' "${validation_config}")

note "checking, linting, and rendering the declared Helm inventory"
while IFS= read -r chart_file; do
  relative_file="${chart_file#"${kubernetes_root}/"}"
  printf '%s\n' "${relative_file%/Chart.yaml}"
done < <(
  find "${kubernetes_root}" \( -type f -o -type l \) -name Chart.yaml -print |
    LC_ALL=C sort
) >"${actual_charts}"
yq eval -r '.spec.helmReleases[].chart' "${validation_config}" | LC_ALL=C sort -u \
  >"${expected_charts}"
if ! diff -u "${expected_charts}" "${actual_charts}"; then
  fail "Helm inventory drifted; declare release, namespace, archive, and controller locks"
fi

chart_count=0
while IFS=$'\t' read -r chart_name release_name release_namespace archive_name \
  archive_digest_variable controller_digest_variable controller_version_variable \
  controller_repository controller_name; do
  [[ -n "${chart_name}" ]] || continue
  require_tool helm "enter the repository's pinned Kubernetes validation environment"
  chart_count=$((chart_count + 1))
  chart_dir="${kubernetes_root}/${chart_name}"
  archive_file="${kubernetes_root}/${archive_name}"
  chart_output="${validation_tmp_dir}/helm-${chart_count}.yaml"

  [[ -f "${chart_dir}/Chart.yaml" ]] || fail "declared Helm chart is missing: ${chart_name}"
  [[ -f "${archive_file}" ]] || fail "${chart_name}: vendored dependency archive is missing"
  [[ "${release_name}" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] ||
    fail "${chart_name}: release name is not a DNS label"
  [[ "${release_namespace}" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] ||
    fail "${chart_name}: release namespace is not a DNS label"

  expected_archive_digest="$(version_value "${archive_digest_variable}")"
  [[ "${expected_archive_digest}" =~ ^sha256:[0-9a-f]{64}$ ]] ||
    fail "${chart_name}: ${archive_digest_variable} is not a sha256 digest"
  actual_archive_digest="sha256:$(sha256_file "${archive_file}")"
  [[ "${actual_archive_digest}" == "${expected_archive_digest}" ]] ||
    fail "${chart_name}: dependency archive digest mismatch (${actual_archive_digest})"

  controller_digest="$(version_value "${controller_digest_variable}")"
  controller_version="$(version_value "${controller_version_variable}")"
  [[ "${controller_digest}" =~ ^sha256:[0-9a-f]{64}$ ]] ||
    fail "${chart_name}: ${controller_digest_variable} is not a sha256 digest"
  expected_controller_image="${controller_repository}:${controller_version}@${controller_digest}"

  helm lint --strict "${chart_dir}"
  helm template "${release_name}" "${chart_dir}" \
    --include-crds \
    --namespace "${release_namespace}" >"${chart_output}"
  chart_resource_count="$(
    yq eval-all '[.] | flatten | map(select(.kind != null and .apiVersion != null)) | length' \
      "${chart_output}"
  )"
  [[ "${chart_resource_count}" != "0" ]] || fail "${chart_name}: Helm chart rendered zero resources"

  controller_count="$(
    yq eval-all '[.] | flatten | map(select(
      .kind == "Deployment" and
      .metadata.namespace == "'"${release_namespace}"'" and
      .metadata.name == "'"${controller_name}"'"
    )) | length' "${chart_output}"
  )"
  [[ "${controller_count}" == "1" ]] ||
    fail "${chart_name}: expected exactly one ${controller_name} Deployment"
  controller_replicas="$(
    yq eval -r 'select(
      .kind == "Deployment" and
      .metadata.namespace == "'"${release_namespace}"'" and
      .metadata.name == "'"${controller_name}"'"
    ) | .spec.replicas' "${chart_output}"
  )"
  [[ "${controller_replicas}" =~ ^[0-9]+$ && "${controller_replicas}" -ge 2 ]] ||
    fail "${chart_name}: ${controller_name} must render at least two replicas"
  unexpected_controller_image_count="$(
    MINDCLADE_EXPECTED_CONTROLLER_IMAGE="${expected_controller_image}" yq eval-all \
      '[.] | flatten | map(select(
        .kind == "Deployment" and
        .metadata.namespace == "'"${release_namespace}"'" and
        .metadata.name == "'"${controller_name}"'"
      )) | map(.spec.template.spec.containers[]) |
      map(select(.image != strenv(MINDCLADE_EXPECTED_CONTROLLER_IMAGE))) | length' \
      "${chart_output}"
  )"
  [[ "${unexpected_controller_image_count}" == "0" ]] ||
    fail "${chart_name}: controller image does not exactly match ${expected_controller_image}"

  pdb_count="$(
    yq eval-all '[.] | flatten | map(select(
      .kind == "PodDisruptionBudget" and
      .metadata.namespace == "'"${release_namespace}"'" and
      .spec.minAvailable == 1
    )) | length' "${chart_output}"
  )"
  [[ "${pdb_count}" != "0" ]] ||
    fail "${chart_name}: controller must render a minAvailable: 1 PodDisruptionBudget"
  certificate_count="$(
    yq eval-all '[.] | flatten | map(select(
      .apiVersion == "cert-manager.io/v1" and
      .kind == "Certificate" and
      .metadata.namespace == "'"${release_namespace}"'"
    )) | length' "${chart_output}"
  )"
  issuer_count="$(
    yq eval-all '[.] | flatten | map(select(
      .apiVersion == "cert-manager.io/v1" and
      (.kind == "Issuer" or .kind == "ClusterIssuer")
    )) | length' "${chart_output}"
  )"
  [[ "${certificate_count}" != "0" && "${issuer_count}" != "0" ]] ||
    fail "${chart_name}: cert-manager Certificate and Issuer resources must be enabled"
  secret_count="$(
    yq eval-all '[.] | flatten | map(select(.kind == "Secret")) | length' "${chart_output}"
  )"
  [[ "${secret_count}" == "0" ]] || fail "${chart_name}: rendered Kubernetes Secrets are forbidden"

  append_rendered_yaml "${chart_output}"
  printf '%s\t%s\n' "${chart_name}" "${chart_output}" >>"${helm_policy_outputs}"
  printf 'HELM              %-65s %s resources\n' "${chart_name}" "${chart_resource_count}"
done < <(
  yq eval -r '.spec.helmReleases[] | [
    .chart,
    .release,
    .namespace,
    .archive,
    .archiveDigestVariable,
    .controllerImageDigestVariable,
    .controllerVersionVariable,
    .controllerImageRepository,
    .controllerName
  ] | @tsv' "${validation_config}"
)
[[ "${chart_count}" != "0" ]] || printf 'No Helm charts declared; Kustomize remains the source format.\n'

note "checking custom-resource kinds against the explicit allowlist"
while IFS=$'\t' read -r api_version resource_kind; do
  [[ -n "${api_version}" && -n "${resource_kind}" ]] || continue
  case "${api_version}" in
    v1 | apps/* | batch/* | autoscaling/* | policy/* | networking.k8s.io/* | \
      rbac.authorization.k8s.io/* | scheduling.k8s.io/* | storage.k8s.io/* | \
      coordination.k8s.io/* | admissionregistration.k8s.io/* | apiregistration.k8s.io/* | \
      apiextensions.k8s.io/* | \
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
  yq eval -r 'select(.kind != null and .apiVersion != null) | [.apiVersion, .kind] | @tsv' \
    "${all_rendered}" | LC_ALL=C sort -u
)

secret_count="$(
  yq eval-all '[.] | flatten | map(select(.kind == "Secret")) | length' "${all_rendered}"
)"
[[ "${secret_count}" == "0" ]] ||
  fail "rendered Kubernetes Secret resources are forbidden; use an external secret contract"

note "checking CustomResourceDefinition structural schemas"
yq eval-all '
  select(
    .apiVersion == "apiextensions.k8s.io/v1" and
    .kind == "CustomResourceDefinition"
  )
' "${all_rendered}" >"${crd_rendered}"

crd_resource_count="$(
  yq eval-all '[.] | flatten | map(select(.kind == "CustomResourceDefinition")) | length' \
    "${crd_rendered}"
)"
crd_violation_count="$(
  yq eval-all '
    [.] | flatten |
    map(select(.kind == "CustomResourceDefinition")) |
    map(select(
      (.spec.group // "") == "" or
      (.spec.names.kind // "") == "" or
      (.spec.names.plural // "") == "" or
      (.spec.scope != "Namespaced" and .spec.scope != "Cluster") or
      (.spec.preserveUnknownFields // false) == true or
      ([.spec.versions[]? | select(.served == true)] | length) == 0 or
      ([.spec.versions[]? | select(.storage == true)] | length) != 1 or
      ([.spec.versions[]? | select((.schema.openAPIV3Schema.type // "") != "object")] | length) != 0
    )) | length
  ' "${crd_rendered}"
)"
[[ "${crd_violation_count}" == "0" ]] ||
  fail "${crd_violation_count} CRD(s) lack a complete structural schema or valid served/storage version"
printf 'Structurally validated %s rendered CustomResourceDefinition(s).\n' "${crd_resource_count}"

note "validating chart-backed custom resources against pinned CRD schemas"
require_tool mkdir "install POSIX file utilities"
mkdir -p "${custom_schema_dir}"
while IFS= read -r custom_gvk; do
  [[ -n "${custom_gvk}" ]] || continue
  custom_kind="${custom_gvk##*/}"
  custom_api_version="${custom_gvk%/*}"
  custom_version="${custom_api_version##*/}"
  custom_group="${custom_api_version%/*}"
  [[ -n "${custom_kind}" && -n "${custom_version}" && -n "${custom_group}" ]] ||
    fail "invalid chart-backed custom GVK: ${custom_gvk}"
  grep -Fqx -- "${custom_gvk}" "${allowed_custom_gvks}" ||
    fail "chart-backed GVK is not in the reviewed custom allowlist: ${custom_gvk}"

  schema_match_count="$(
    MINDCLADE_SCHEMA_GROUP="${custom_group}" \
      MINDCLADE_SCHEMA_VERSION="${custom_version}" \
      MINDCLADE_SCHEMA_KIND="${custom_kind}" \
      yq eval-all '[.] | flatten |
        map(select(
          .kind == "CustomResourceDefinition" and
          .spec.group == strenv(MINDCLADE_SCHEMA_GROUP) and
          .spec.names.kind == strenv(MINDCLADE_SCHEMA_KIND)
        )) |
        map(.spec.versions[] | select(.name == strenv(MINDCLADE_SCHEMA_VERSION))) |
        length' "${crd_rendered}"
  )"
  [[ "${schema_match_count}" == "1" ]] ||
    fail "${custom_gvk}: expected exactly one schema in the pinned chart CRDs"

  custom_schema_file="${custom_schema_dir}/${custom_kind}_${custom_group}_${custom_version}.json"
  MINDCLADE_SCHEMA_GROUP="${custom_group}" \
    MINDCLADE_SCHEMA_VERSION="${custom_version}" \
    MINDCLADE_SCHEMA_KIND="${custom_kind}" \
    yq eval-all -o=json -I=2 '
      [.] | flatten |
      map(select(
        .kind == "CustomResourceDefinition" and
        .spec.group == strenv(MINDCLADE_SCHEMA_GROUP) and
        .spec.names.kind == strenv(MINDCLADE_SCHEMA_KIND)
      )) | .[0].spec.versions[] |
      select(.name == strenv(MINDCLADE_SCHEMA_VERSION)) |
      .schema.openAPIV3Schema
    ' "${crd_rendered}" >"${custom_schema_file}"

  custom_instance_count="$(
    MINDCLADE_CUSTOM_API_VERSION="${custom_api_version}" \
      MINDCLADE_CUSTOM_KIND="${custom_kind}" \
      yq eval-all '[.] | flatten | map(select(
        .apiVersion == strenv(MINDCLADE_CUSTOM_API_VERSION) and
        .kind == strenv(MINDCLADE_CUSTOM_KIND)
      )) | length' "${all_rendered}"
  )"
  [[ "${custom_instance_count}" != "0" ]] ||
    fail "${custom_gvk}: pinned CRD is present but no contract instance is rendered"
  printf -- '---\n' >>"${chart_custom_rendered}"
  MINDCLADE_CUSTOM_API_VERSION="${custom_api_version}" \
    MINDCLADE_CUSTOM_KIND="${custom_kind}" \
    yq eval 'select(
      .apiVersion == strenv(MINDCLADE_CUSTOM_API_VERSION) and
      .kind == strenv(MINDCLADE_CUSTOM_KIND)
    )' "${all_rendered}" >>"${chart_custom_rendered}"
done < <(yq eval -r '.spec.chartBackedCustomGVKs[]' "${validation_config}")

chart_custom_resource_count="$(
  yq eval-all '[.] | flatten | map(select(.kind != null and .apiVersion != null)) | length' \
    "${chart_custom_rendered}"
)"
[[ "${chart_custom_resource_count}" != "0" ]] ||
  fail "chart-backed custom-resource validation selected zero resources"
kubeconform \
  -exit-on-error \
  -schema-location "${custom_schema_dir}/{{.ResourceKind}}_{{.Group}}_{{.ResourceAPIVersion}}.json" \
  -strict \
  -summary \
  "${chart_custom_rendered}"

note "checking cluster and namespace scope invariants"
while IFS=$'\t' read -r api_version resource_kind resource_name resource_namespace; do
  [[ -n "${api_version}${resource_kind}${resource_name}${resource_namespace}" ]] || continue
  [[ -n "${api_version}" && -n "${resource_kind}" && -n "${resource_name}" ]] ||
    fail "scope record is incomplete: apiVersion='${api_version}', kind='${resource_kind}', name='${resource_name}'"
  resource_gvk="${api_version}/${resource_kind}"
  if grep -Fqx -- "${resource_gvk}" "${cluster_scoped_gvks}"; then
    [[ -z "${resource_namespace}" ]] ||
      fail "${resource_gvk}/${resource_name}: cluster-scoped resource must not set metadata.namespace"
  else
    [[ -n "${resource_namespace}" ]] ||
      fail "${resource_gvk}/${resource_name}: namespaced resource must set metadata.namespace explicitly"
  fi
done < <(
  yq eval -r '
    select(.kind != null and .apiVersion != null) |
    [.apiVersion, .kind, (.metadata.name // ""), (.metadata.namespace // "")] | @tsv
  ' "${all_rendered}"
)

note "validating built-in Kubernetes resources against ${kubernetes_version} schemas"
yq eval-all '
  select(
    .kind != null and .apiVersion != null and
    (
      .apiVersion != "apiextensions.k8s.io/v1" or
      .kind != "CustomResourceDefinition"
    ) and
    (
      .apiVersion == "v1" or
      (.apiVersion | test("^(apps|batch|autoscaling|policy|networking.k8s.io|rbac.authorization.k8s.io|scheduling.k8s.io|storage.k8s.io|coordination.k8s.io|admissionregistration.k8s.io|apiregistration.k8s.io|apiextensions.k8s.io|authentication.k8s.io|authorization.k8s.io|certificates.k8s.io|discovery.k8s.io|events.k8s.io|flowcontrol.apiserver.k8s.io|node.k8s.io|resource.k8s.io)/"))
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
while IFS=$'\t' read -r chart_name chart_output; do
  [[ -s "${chart_output}" ]] || fail "Helm policy input is missing: ${chart_name}"
  conftest test \
    --no-color \
    --policy "${policy_dir}" \
    --rego-version v1 \
    --strict \
    "${chart_output}"

  duplicate_identity_count="$(
    yq eval-all '[.] | flatten |
      map(select(.kind != null and .metadata.name != null)) |
      group_by(.apiVersion + "/" + .kind + "/" + (.metadata.namespace // "_cluster") + "/" + .metadata.name) |
      map(select(length > 1)) | length' "${chart_output}"
  )"
  [[ "${duplicate_identity_count}" == "0" ]] ||
    fail "${chart_name}: rendered Helm output contains duplicate resource identities"
  printf 'POLICY-HELM       %s\n' "${chart_name}"
done <"${helm_policy_outputs}"

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
  if [[ "${workload_count}" != "0" ]] && grep -Fqx -- "${root_name}" "${network_policy_roots}"; then
    default_deny_count="$(
      yq eval-all '[.] | flatten | map(select(
        .kind == "NetworkPolicy" and
        .spec.podSelector != null and
        (.spec.policyTypes | contains(["Ingress"])) and
        (.spec.policyTypes | contains(["Egress"])) and
        ((.spec.ingress // []) | length) == 0 and
        ((.spec.egress // []) | length) == 0
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
done <"${policy_roots}"

note "Kubernetes validation passed"
printf 'Validated %s built-in rendered resources, %s Helm chart(s), and every declared Kustomize root.\n' \
  "${core_resource_count}" "${chart_count}"
