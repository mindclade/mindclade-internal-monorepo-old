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
repository_root="$(cd -- "${kubernetes_root}/../.." && pwd)"

# Keep the direct command useful while the action-hermetic bridge is transitional: Nix supplies
# the pinned CLI closure and Bazel remains the one canonical validation implementation.
if [[ "${MINDCLADE_VALIDATION_INTERNAL:-}" != "1" ]]; then
  exec nix develop "${repository_root}#ci" --command "${repository_root}/tools/dev/bazelw" test \
    //infra/kubernetes:validate --test_output=errors
fi

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
require_tool tr "install POSIX text utilities"
require_tool promtool "enter the repository's pinned Kubernetes validation environment"
require_tool python3 "enter the repository's pinned Kubernetes validation environment"
require_tool yq "enter the repository's pinned Kubernetes validation environment"

[[ -f "${validation_config}" ]] || fail "validation configuration is missing: ${validation_config}"
[[ -d "${policy_dir}" ]] || fail "Conftest policy directory is missing: ${policy_dir}"
[[ -f "${version_lock}" ]] || fail "Kubernetes version lock is missing: ${version_lock}"
[[ -d "${MINDCLADE_KUBERNETES_SCHEMA_DIR:-}" ]] ||
  fail "declared local Kubernetes schema directory is missing"
[[ -d "${MINDCLADE_CUSTOM_CRD_SCHEMA_DIR:-}" ]] ||
  fail "declared custom CRD schema directory is missing"
[[ -f "${MINDCLADE_TOOLCHAIN_MANIFEST:-}" ]] ||
  fail "declared Nix toolchain manifest is missing"

# The validator uses mikefarah/yq syntax. Python yq accepts many of these expressions but gives
# different exit semantics, which can turn an empty selection into a false success.
yq_version="$(yq --version 2>&1)"
[[ "${yq_version}" == *"mikefarah/yq"* ]] ||
  fail "unsupported yq implementation (${yq_version}); mikefarah/yq v4 is required"

verify_tool_version() {
  local tool_name="$1"
  local manifest_path="$2"
  local actual_version="$3"
  local expected_version

  expected_version="$(yq eval -r "${manifest_path}" "${MINDCLADE_TOOLCHAIN_MANIFEST}")"
  [[ -n "${expected_version}" && "${expected_version}" != "null" ]] ||
    fail "toolchain manifest has no version for ${tool_name}"
  [[ "${actual_version}" == *"${expected_version}"* ]] ||
    fail "${tool_name} version drift: expected ${expected_version}, found ${actual_version}"
}

verify_tool_version conftest '.ciTools.conftest' "$(conftest --version 2>&1)"
verify_tool_version helm '.ciTools.helm' "$(helm version --short 2>&1)"
verify_tool_version kubeconform '.ciTools.kubeconform' "$(kubeconform -v 2>&1)"
verify_tool_version kustomize '.ciTools.kustomize' "$(kustomize version 2>&1)"
verify_tool_version promtool '.ciTools.promtool' "$(promtool --version 2>&1)"
verify_tool_version python3 '.tools.python' "$(python3 --version 2>&1)"
verify_tool_version yq '.ciTools.yq' "${yq_version}"

kubernetes_version="$(yq eval -r '.spec.kubernetesVersion' "${validation_config}")"
[[ "${kubernetes_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
  fail "spec.kubernetesVersion must be an explicit major.minor.patch value"
locked_kubernetes_version="$(version_value MINDCLADE_KUBERNETES_VERSION)"
locked_kubernetes_version="${locked_kubernetes_version%%-gke.*}"
[[ "${kubernetes_version}" == "${locked_kubernetes_version}" ]] ||
  fail "schema version ${kubernetes_version} does not match versions.env ${locked_kubernetes_version}"

validation_tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/mindclade-kubernetes-validation.XXXXXX")"
cleanup() {
  # The target is an explicit directory returned by mktemp above, never a caller-supplied path.
  rm -rf -- "${validation_tmp_dir}"
}
trap cleanup EXIT

actual_roots="${validation_tmp_dir}/actual-roots.txt"
expected_roots="${validation_tmp_dir}/expected-roots.txt"
actual_charts="${validation_tmp_dir}/actual-charts.txt"
expected_charts="${validation_tmp_dir}/expected-charts.txt"
allowed_custom_gvks="${validation_tmp_dir}/allowed-custom-gvks.txt"
cluster_scoped_gvks="${validation_tmp_dir}/cluster-scoped-gvks.txt"
policy_roots="${validation_tmp_dir}/policy-roots.txt"
network_policy_roots="${validation_tmp_dir}/network-policy-roots.txt"
namespace_wide_deny_roots="${validation_tmp_dir}/namespace-wide-deny-roots.txt"
helm_policy_outputs="${validation_tmp_dir}/helm-policy-outputs.txt"
all_rendered="${validation_tmp_dir}/all-rendered.yaml"
core_rendered="${validation_tmp_dir}/core-rendered.yaml"
crd_rendered="${validation_tmp_dir}/crd-rendered.yaml"
chart_custom_rendered="${validation_tmp_dir}/chart-custom-rendered.yaml"
custom_schema_dir="${validation_tmp_dir}/custom-schemas"
external_custom_rendered="${validation_tmp_dir}/external-custom-rendered.yaml"
: >"${all_rendered}"
: >"${helm_policy_outputs}"
: >"${chart_custom_rendered}"
: >"${external_custom_rendered}"

note "checking the declared Kustomize inventory"
while IFS= read -r kustomization_file; do
  kustomization_kind="$(yq eval -r '.kind // ""' "${kustomization_file}")"
  [[ "${kustomization_kind}" == "Component" ]] && continue
  [[ "${kustomization_kind}" == "Kustomization" ]] || {
    printf '__INVALID__/%s\n' "${kustomization_file#"${kubernetes_root}/"}"
    continue
  }
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
yq eval -r '.spec.namespaceWideDefaultDenyRoots[]' "${validation_config}" | LC_ALL=C sort -u \
  >"${namespace_wide_deny_roots}"
cross_resource_args=()
while IFS= read -r unresolved_service_ref; do
  [[ -n "${unresolved_service_ref}" ]] || continue
  cross_resource_args+=(--allow-unresolved-service-ref "${unresolved_service_ref}")
done < <(yq eval -r '.spec.activationGatedUnresolvedServiceRefs[]' "${validation_config}")
cross_root_resource_args=()
while IFS= read -r cross_root_service_ref; do
  [[ -n "${cross_root_service_ref}" ]] || continue
  cross_root_resource_args+=(--allow-unresolved-service-ref "${cross_root_service_ref}")
done < <(yq eval -r '.spec.crossRootServiceRefs[]' "${validation_config}")
while IFS= read -r unmatched_network_policy; do
  [[ -n "${unmatched_network_policy}" ]] || continue
  cross_resource_args+=(--allow-unmatched-network-policy "${unmatched_network_policy}")
done < <(yq eval -r '.spec.activationGatedUnmatchedNetworkPolicies[]' "${validation_config}")
while IFS= read -r root_name; do
  grep -Fqx -- "${root_name}" "${policy_roots}" ||
    fail "network-policy root is not also a policy root: ${root_name}"
done <"${network_policy_roots}"
while IFS= read -r root_name; do
  grep -Fqx -- "${root_name}" "${network_policy_roots}" ||
    fail "namespace-wide default-deny root is not a network-policy root: ${root_name}"
done <"${namespace_wide_deny_roots}"

check_local_resources() {
  local root_name="$1"
  local kustomization_file="${kubernetes_root}/${root_name}/kustomization.yaml"
  local remote_reference_count

  # Scan every scalar, not only resources: components, generators, transformers,
  # configurations, CRD configuration and Helm repositories are all fetch-capable inputs.
  remote_reference_count="$(
    yq eval '[.. | select(tag == "!!str") |
      select(test("^(https?://|git::|git@|oci://|s3://|github\\.com/)|[?&]ref="))] | length' \
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
{
  yq eval -r '.spec.helmReleases[]?.chart' "${validation_config}"
  yq eval -r '.spec.applicationHelmReleases[]?.chart' "${validation_config}"
} | LC_ALL=C sort -u >"${expected_charts}"
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

application_chart_count=0
while IFS=$'\t' read -r chart_name release_name release_namespace values_name lock_name \
  workload_name expected_resource_count; do
  [[ -n "${chart_name}" ]] || continue
  application_chart_count=$((application_chart_count + 1))
  chart_dir="${kubernetes_root}/${chart_name}"
  values_file="${kubernetes_root}/${values_name}"
  lock_file="${repository_root}/${lock_name}"
  disabled_output="${validation_tmp_dir}/application-helm-disabled-${application_chart_count}.yaml"
  chart_output="${validation_tmp_dir}/application-helm-${application_chart_count}.yaml"

  [[ -f "${chart_dir}/Chart.yaml" ]] || fail "declared application Helm chart is missing: ${chart_name}"
  [[ -f "${values_file}" ]] || fail "${chart_name}: qualification values are missing"
  [[ -f "${lock_file}" ]] || fail "${chart_name}: runtime image lock is missing"
  [[ "${release_name}" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] ||
    fail "${chart_name}: release name is not a DNS label"
  [[ "${release_namespace}" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] ||
    fail "${chart_name}: release namespace is not a DNS label"
  [[ "${workload_name}" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] ||
    fail "${chart_name}: workload name is not a DNS label"
  [[ "${expected_resource_count}" =~ ^[1-9][0-9]*$ ]] ||
    fail "${chart_name}: expected resource count is invalid"

  dependency_count="$(yq eval -r '(.dependencies // []) | length' "${chart_dir}/Chart.yaml")"
  [[ "${dependency_count}" == "0" ]] ||
    fail "${chart_name}: application chart dependencies must be vendored and explicitly locked"

  [[ "$(yq eval -r '.kind' "${lock_file}")" == "RuntimeImageQualificationLock" ]] ||
    fail "${chart_name}: runtime image lock kind is invalid"
  locked_target="$(yq eval -r '.spec.target' "${lock_file}")"
  locked_platform="$(yq eval -r '.spec.platform' "${lock_file}")"
  locked_repository_suffix="$(yq eval -r '.spec.repositorySuffix' "${lock_file}")"
  locked_digest="$(yq eval -r '.spec.imageDigest' "${lock_file}")"
  locked_version="$(yq eval -r '.spec.mlflow.version' "${lock_file}")"
  locked_requirements_name="$(yq eval -r '.spec.mlflow.requirementsLock' "${lock_file}")"
  locked_requirements_digest="$(yq eval -r '.spec.mlflow.requirementsDigest' "${lock_file}")"
  [[ "${locked_target}" == "//services/mlflow:image" ]] ||
    fail "${chart_name}: runtime image lock target is invalid"
  [[ "${locked_platform}" == "linux/amd64" ]] ||
    fail "${chart_name}: runtime image lock platform is invalid"
  [[ "${locked_repository_suffix}" == "/mlflow-server" ]] ||
    fail "${chart_name}: runtime image lock repository suffix is invalid"
  [[ "${locked_digest}" =~ ^sha256:[0-9a-f]{64}$ ]] ||
    fail "${chart_name}: runtime image lock has an invalid digest"
  [[ "$(yq eval -r '.appVersion' "${chart_dir}/Chart.yaml")" == "${locked_version}" ]] ||
    fail "${chart_name}: Chart appVersion does not match the runtime image lock"
  [[ "${locked_requirements_name}" =~ ^services/mlflow/[A-Za-z0-9._/-]+$ &&
    "${locked_requirements_name}" != *"../"* ]] ||
    fail "${chart_name}: requirements lock path is unsafe"
  requirements_lock_file="${repository_root}/${locked_requirements_name}"
  [[ -f "${requirements_lock_file}" ]] ||
    fail "${chart_name}: declared requirements lock is missing"
  [[ "${locked_requirements_digest}" =~ ^sha256:[0-9a-f]{64}$ ]] ||
    fail "${chart_name}: requirements lock digest is invalid"
  actual_requirements_digest="sha256:$(sha256_file "${requirements_lock_file}")"
  [[ "${actual_requirements_digest}" == "${locked_requirements_digest}" ]] ||
    fail "${chart_name}: requirements lock digest drifted"
  qualification_repository="$(yq eval -r '.image.repository' "${values_file}")"
  [[ "${qualification_repository}" == *"${locked_repository_suffix}" ]] ||
    fail "${chart_name}: qualification values do not select the Mindclade wrapper image"
  expected_image="${qualification_repository}@${locked_digest}"

  helm lint --strict "${chart_dir}"
  helm template "${release_name}" "${chart_dir}" \
    --namespace "${release_namespace}" >"${disabled_output}"
  disabled_resource_count="$(
    yq eval-all '[.] | flatten | map(select(.kind != null and .apiVersion != null)) | length' \
      "${disabled_output}"
  )"
  [[ "${disabled_resource_count}" == "0" ]] ||
    fail "${chart_name}: default application values must render zero resources"

  helm lint --strict "${chart_dir}" --values "${values_file}"
  helm template "${release_name}" "${chart_dir}" \
    --namespace "${release_namespace}" \
    --values "${values_file}" >"${chart_output}"
  chart_resource_count="$(
    yq eval-all '[.] | flatten | map(select(.kind != null and .apiVersion != null)) | length' \
      "${chart_output}"
  )"
  [[ "${chart_resource_count}" == "${expected_resource_count}" ]] ||
    fail "${chart_name}: expected ${expected_resource_count} qualified resources, found ${chart_resource_count}"

  deployment_count="$(yq eval-all '[.] | flatten | map(select(
    .apiVersion == "apps/v1" and .kind == "Deployment" and
    .metadata.namespace == "'"${release_namespace}"'" and
    .metadata.name == "'"${workload_name}"'"
  )) | length' "${chart_output}")"
  [[ "${deployment_count}" == "1" ]] ||
    fail "${chart_name}: expected exactly one ${workload_name} Deployment"
  deployment_image="$(yq eval -r 'select(
    .apiVersion == "apps/v1" and .kind == "Deployment" and
    .metadata.namespace == "'"${release_namespace}"'" and
    .metadata.name == "'"${workload_name}"'"
  ) | .spec.template.spec.containers[] | select(.name == "mlflow") | .image' "${chart_output}")"
  [[ "${deployment_image}" == "${expected_image}" ]] ||
    fail "${chart_name}: workload image does not exactly match ${expected_image}"
  release_evidence_digest="$(yq eval -r 'select(
    .apiVersion == "apps/v1" and .kind == "Deployment" and
    .metadata.namespace == "'"${release_namespace}"'" and
    .metadata.name == "'"${workload_name}"'"
  ) | .spec.template.metadata.annotations."mindclade.dev/release-evidence-digest"' "${chart_output}")"
  [[ "${release_evidence_digest}" =~ ^sha256:[0-9a-f]{64}$ &&
    "${release_evidence_digest}" != "sha256:0000000000000000000000000000000000000000000000000000000000000000" ]] ||
    fail "${chart_name}: activation requires a nonzero release evidence digest"

  forbidden_resource_count="$(yq eval-all '[.] | flatten | map(select(
    .kind == "Secret" or .kind == "PersistentVolumeClaim" or
    .kind == "Role" or .kind == "RoleBinding" or
    .kind == "ClusterRole" or .kind == "ClusterRoleBinding" or
    .kind == "CustomResourceDefinition" or .kind == "Namespace" or
    (.kind == "Service" and (.spec.type == "LoadBalancer" or .spec.type == "NodePort" or .spec.type == "ExternalName"))
  )) | length' "${chart_output}")"
  [[ "${forbidden_resource_count}" == "0" ]] ||
    fail "${chart_name}: application chart rendered a secret, storage claim, RBAC, cluster-scoped object, or external service"

  append_rendered_yaml "${chart_output}"
  printf '%s\t%s\n' "${chart_name}" "${chart_output}" >>"${helm_policy_outputs}"
  printf 'HELM-APPLICATION  %-65s %s resources (default: blocked)\n' \
    "${chart_name}" "${chart_resource_count}"
done < <(
  yq eval -r '.spec.applicationHelmReleases[]? | [
    .chart,
    .release,
    .namespace,
    .values,
    .runtimeImageLock,
    .workloadName,
    .expectedResourceCount
  ] | @tsv' "${validation_config}"
)

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

  # Kubeconform normalizes ResourceKind to lowercase before expanding schema-location.
  # macOS' usual case-insensitive filesystem can hide a mismatched filename; normalize here so
  # the same generated registry is valid on Linux runners and case-sensitive developer volumes.
  custom_kind_lower="$(printf '%s' "${custom_kind}" | LC_ALL=C tr '[:upper:]' '[:lower:]')"
  custom_schema_file="${custom_schema_dir}/${custom_kind_lower}_${custom_group}_${custom_version}.json"
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

note "validating managed custom resources against fixed-output CRD schemas"
while IFS=$'\t' read -r custom_gvk schema_file; do
  [[ -n "${custom_gvk}" && -n "${schema_file}" ]] || continue
  custom_kind="${custom_gvk##*/}"
  custom_api_version="${custom_gvk%/*}"
  custom_version="${custom_api_version##*/}"
  custom_group="${custom_api_version%/*}"
  source_crd="${MINDCLADE_CUSTOM_CRD_SCHEMA_DIR}/${schema_file}"
  [[ -f "${source_crd}" ]] || fail "${custom_gvk}: pinned CRD file is missing: ${schema_file}"
  grep -Fqx -- "${custom_gvk}" "${allowed_custom_gvks}" ||
    fail "${custom_gvk}: pinned schema is not in the reviewed custom allowlist"

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
        length' \
        "${source_crd}"
  )"
  [[ "${schema_match_count}" == "1" ]] ||
    fail "${custom_gvk}: pinned CRD must contain exactly one matching structural schema"

  custom_kind_lower="$(printf '%s' "${custom_kind}" | LC_ALL=C tr '[:upper:]' '[:lower:]')"
  custom_schema_file="${custom_schema_dir}/${custom_kind_lower}_${custom_group}_${custom_version}.json"
  MINDCLADE_SCHEMA_GROUP="${custom_group}" \
    MINDCLADE_SCHEMA_VERSION="${custom_version}" \
    MINDCLADE_SCHEMA_KIND="${custom_kind}" \
    yq eval-all -o=json -I=2 \
    '[.] | flatten |
      map(select(
        .kind == "CustomResourceDefinition" and
        .spec.group == strenv(MINDCLADE_SCHEMA_GROUP) and
        .spec.names.kind == strenv(MINDCLADE_SCHEMA_KIND)
      )) |
      map(.spec.versions[] | select(.name == strenv(MINDCLADE_SCHEMA_VERSION))) |
      .[0].schema.openAPIV3Schema' \
    "${source_crd}" >"${custom_schema_file}"

  printf -- '---\n' >>"${external_custom_rendered}"
  MINDCLADE_CUSTOM_API_VERSION="${custom_api_version}" \
    MINDCLADE_CUSTOM_KIND="${custom_kind}" \
    yq eval 'select(
      .apiVersion == strenv(MINDCLADE_CUSTOM_API_VERSION) and
      .kind == strenv(MINDCLADE_CUSTOM_KIND)
    )' "${all_rendered}" >>"${external_custom_rendered}"
done < <(yq eval -r '.spec.pinnedExternalCustomSchemas[] | [.gvk, .file] | @tsv' \
  "${validation_config}")

reviewed_schema_gvks="${validation_tmp_dir}/reviewed-schema-gvks.txt"
{
  yq eval -r '.spec.chartBackedCustomGVKs[]' "${validation_config}"
  yq eval -r '.spec.pinnedExternalCustomSchemas[].gvk' "${validation_config}"
} | LC_ALL=C sort -u >"${reviewed_schema_gvks}"
if ! diff -u "${allowed_custom_gvks}" "${reviewed_schema_gvks}"; then
  fail "every allowed custom GVK must have an exact chart-backed or fixed-output schema"
fi

external_custom_count="$(
  yq eval-all '[.] | flatten | map(select(.kind != null and .apiVersion != null)) | length' \
    "${external_custom_rendered}"
)"
[[ "${external_custom_count}" != "0" ]] ||
  fail "fixed-output custom schema validation selected zero resources"
kubeconform \
  -exit-on-error \
  -schema-location "${custom_schema_dir}/{{.ResourceKind}}_{{.Group}}_{{.ResourceAPIVersion}}.json" \
  -strict \
  -summary \
  "${external_custom_rendered}"

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

note "checking exact admission-policy and protected-namespace contracts"
security_render="${validation_tmp_dir}/platform__security.yaml"
[[ -s "${security_render}" ]] || fail "platform/security did not produce a render"
vap_count="$(yq eval-all '[.] | flatten | map(select(.kind == "ValidatingAdmissionPolicy")) | length' \
  "${security_render}")"
binding_count="$(yq eval-all '[.] | flatten | map(select(.kind == "ValidatingAdmissionPolicyBinding")) | length' \
  "${security_render}")"
[[ "${vap_count}" == "7" && "${binding_count}" == "7" ]] ||
  fail "platform/security must render exactly seven reviewed VAPs and seven bindings"

expected_vap_matrix='[{"name":"mindclade-block-deployment-activation","failure":"Fail","groups":[["apps"]],"resources":[["deployments","statefulsets"]],"operations":[["CREATE","UPDATE"]]},{"name":"mindclade-block-job-activation","failure":"Fail","groups":[["batch"],["jobset.x-k8s.io"]],"resources":[["jobs"],["jobsets"]],"operations":[["CREATE","UPDATE"],["CREATE","UPDATE"]]},{"name":"mindclade-capacity-contract-object","failure":"Fail","groups":[[""]],"resources":[["configmaps"]],"operations":[["CREATE","UPDATE"]]},{"name":"mindclade-capacity-namespace-activation","failure":"Fail","groups":[[""]],"resources":[["namespaces"]],"operations":[["CREATE","UPDATE"]]},{"name":"mindclade-capacity-queue-contract","failure":"Fail","groups":[["batch"],["jobset.x-k8s.io"]],"resources":[["jobs"],["jobsets"]],"operations":[["CREATE","UPDATE"],["CREATE","UPDATE"]]},{"name":"mindclade-internal-services","failure":"Fail","groups":[[""]],"resources":[["services"]],"operations":[["CREATE","UPDATE"]]},{"name":"mindclade-restricted-pods","failure":"Fail","groups":[[""]],"resources":[["pods"]],"operations":[["CREATE","UPDATE"]]}]'
actual_vap_matrix="$(yq eval-all -o=json -I=0 '
  [.] | flatten | map(select(.kind == "ValidatingAdmissionPolicy")) |
  map({
    "name": .metadata.name,
    "failure": .spec.failurePolicy,
    "groups": [.spec.matchConstraints.resourceRules[].apiGroups],
    "resources": [.spec.matchConstraints.resourceRules[].resources],
    "operations": [.spec.matchConstraints.resourceRules[].operations]
  }) | sort_by(.name)
' "${security_render}")"
[[ "${actual_vap_matrix}" == "${expected_vap_matrix}" ]] ||
  fail "ValidatingAdmissionPolicy name, failure policy, operations, API group, or resource matrix drifted"

expected_binding_matrix='[{"name":"mindclade-block-deployment-activation","policy":"mindclade-block-deployment-activation","actions":["Audit","Deny"],"labels":{"mindclade.dev/workload-activation":"blocked"},"expressions":[]},{"name":"mindclade-block-job-activation","policy":"mindclade-block-job-activation","actions":["Audit","Deny"],"labels":{"mindclade.dev/workload-activation":"blocked"},"expressions":[]},{"name":"mindclade-capacity-contract-object","policy":"mindclade-capacity-contract-object","actions":["Audit","Deny"],"labels":{"mindclade.dev/queue-enforcement":"enforced"},"expressions":[]},{"name":"mindclade-capacity-namespace-activation","policy":"mindclade-capacity-namespace-activation","actions":["Audit","Deny"],"labels":{},"expressions":[]},{"name":"mindclade-capacity-queue-contract","policy":"mindclade-capacity-queue-contract","actions":["Audit","Deny"],"labels":{"mindclade.dev/queue-enforcement":"enforced"},"expressions":[]},{"name":"mindclade-internal-services","policy":"mindclade-internal-services","actions":["Audit","Deny"],"labels":{"mindclade.dev/admission":"enforced"},"expressions":[]},{"name":"mindclade-restricted-pods","policy":"mindclade-restricted-pods","actions":["Audit","Deny"],"labels":{"mindclade.dev/admission":"enforced"},"expressions":[]}]'
actual_binding_matrix="$(yq eval-all -o=json -I=0 '
  [.] | flatten | map(select(.kind == "ValidatingAdmissionPolicyBinding")) |
  map({
    "name": .metadata.name,
    "policy": .spec.policyName,
    "actions": (.spec.validationActions | sort),
    "labels": (.spec.matchResources.namespaceSelector.matchLabels // {}),
    "expressions": (.spec.matchResources.namespaceSelector.matchExpressions // [])
  }) | sort_by(.name)
' "${security_render}")"
[[ "${actual_binding_matrix}" == "${expected_binding_matrix}" ]] ||
  fail "ValidatingAdmissionPolicyBinding name, policy, action, or selector matrix drifted"

vap_violation_count="$(yq eval-all '
  [.] | flatten |
  map(select(.kind == "ValidatingAdmissionPolicy")) |
  map(select(
    .spec.failurePolicy != "Fail" or
    ([.spec.matchConstraints.resourceRules[]?.operations[]? | select(. != "CREATE" and . != "UPDATE")] | length) != 0 or
    ([.spec.matchConstraints.resourceRules[]? | select((.operations | contains(["CREATE", "UPDATE"])) == false)] | length) != 0 or
    ((.spec.validations // []) | length) == 0
  )) | length
' "${security_render}")"
[[ "${vap_violation_count}" == "0" ]] ||
  fail "every VAP must fail closed, cover CREATE+UPDATE, and contain validations"

protected_namespace_violation_count="$(yq eval-all '
  [.] | flatten |
  map(select(
    .kind == "Namespace" and
    .metadata.labels."mindclade.dev/admission" == "enforced"
  )) |
  map(select(
    .metadata.labels."mindclade.dev/workload-activation" != "blocked" or
    .metadata.labels."pod-security.kubernetes.io/enforce" != "restricted" or
    .metadata.labels."pod-security.kubernetes.io/enforce-version" != "v1.36" or
    .metadata.labels."pod-security.kubernetes.io/audit" != "restricted" or
    .metadata.labels."pod-security.kubernetes.io/audit-version" != "v1.36" or
    .metadata.labels."pod-security.kubernetes.io/warn" != "restricted" or
    .metadata.labels."pod-security.kubernetes.io/warn-version" != "v1.36"
  )) | length
' "${all_rendered}")"
[[ "${protected_namespace_violation_count}" == "0" ]] ||
  fail "protected namespaces must stay activation-blocked with exact restricted/v1.36 PSS labels"

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
  -schema-location "${MINDCLADE_KUBERNETES_SCHEMA_DIR}/{{.ResourceKind}}{{.KindSuffix}}.json" \
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
grep -Fq 'must not set externalIPs' "${negative_policy_output}" ||
  fail "negative policy fixture did not exercise Service externalIPs parity"
grep -Fq 'must not define ephemeral containers' "${negative_policy_output}" ||
  fail "negative policy fixture did not exercise ephemeral-container parity"

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
  normalized_chart_output="${chart_output%.yaml}.json"
  yq eval-all -o=json -I=0 '[.] | flatten | map(select(.kind != null and .apiVersion != null))' \
    "${chart_output}" >"${normalized_chart_output}"
  python3 "${script_dir}/cross_resource.py" "${normalized_chart_output}" \
    --label "${chart_name}" "${cross_resource_args[@]}"
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
  if { [[ "${workload_count}" != "0" ]] || grep -Fqx -- "${root_name}" "${namespace_wide_deny_roots}"; } &&
    grep -Fqx -- "${root_name}" "${network_policy_roots}"; then
    require_namespace_wide=false
    if grep -Fqx -- "${root_name}" "${namespace_wide_deny_roots}"; then
      require_namespace_wide=true
    fi
    default_deny_count="$(
      MINDCLADE_REQUIRE_NAMESPACE_WIDE="${require_namespace_wide}" yq eval-all '[.] | flatten | map(select(
        .kind == "NetworkPolicy" and
        ([.spec.policyTypes[]? | select(. == "Ingress")] | length) == 1 and
        ([.spec.policyTypes[]? | select(. == "Egress")] | length) == 1 and
        ((.spec.ingress // []) | length) == 0 and
        ((.spec.egress // []) | length) == 0 and
        (strenv(MINDCLADE_REQUIRE_NAMESPACE_WIDE) != "true" or
          ((.spec.podSelector | type) == "!!map" and (.spec.podSelector | length) == 0))
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

  normalized_output="${validation_tmp_dir}/${output_name}.json"
  yq eval-all -o=json -I=0 '[.] | flatten | map(select(.kind != null and .apiVersion != null))' \
    "${output_file}" >"${normalized_output}"
  python3 "${script_dir}/cross_resource.py" "${normalized_output}" --label "${root_name}" \
    "${cross_resource_args[@]}" "${cross_root_resource_args[@]}"

  printf 'POLICY            %s\n' "${root_name}"
done <"${policy_roots}"

note "checking global references and fail-closed capacity contracts"
combined_json="${validation_tmp_dir}/all-rendered.json"
yq eval-all -o=json -I=0 '[.] | flatten | map(select(.kind != null and .apiVersion != null))' \
  "${all_rendered}" >"${combined_json}"
python3 "${script_dir}/cross_resource.py" "${combined_json}" \
  --label "combined inventory" "${cross_resource_args[@]}"

capacity_args=()
for capacity_workload_root in \
  workloads/ingestion \
  workloads/preprocessing \
  workloads/training/overlays/h100 \
  workloads/training/overlays/b200; do
  capacity_workload_name="${capacity_workload_root//\//__}"
  capacity_workload_json="${validation_tmp_dir}/${capacity_workload_name}.json"
  yq eval-all -o=json -I=0 '[.] | flatten | map(select(.kind != null and .apiVersion != null))' \
    "${validation_tmp_dir}/${capacity_workload_name}.yaml" >"${capacity_workload_json}"
  capacity_args+=(--workloads "${capacity_workload_json}")
done
for capacity_root in base policies platform/kueue; do
  capacity_name="${capacity_root//\//__}"
  yq eval-all -o=json -I=0 '[.] | flatten | map(select(.kind != null and .apiVersion != null))' \
    "${validation_tmp_dir}/${capacity_name}.yaml" >"${validation_tmp_dir}/${capacity_name}.json"
done
python3 "${script_dir}/capacity_contract.py" \
  --base "${validation_tmp_dir}/base.json" \
  --policies "${validation_tmp_dir}/policies.json" \
  --queues "${validation_tmp_dir}/platform__kueue.json" \
  --all "${combined_json}" \
  "${capacity_args[@]}"

qualification_json="${validation_tmp_dir}/platform__qualification.json"
yq eval-all -o=json -I=0 '[.] | flatten | map(select(.kind != null and .apiVersion != null))' \
  "${validation_tmp_dir}/platform__qualification.yaml" >"${qualification_json}"
python3 "${script_dir}/qualification_contract.py" "${qualification_json}"

python3 "${script_dir}/training_qualification_profile.py" \
  --workloads "${validation_tmp_dir}/workloads__training__overlays__h100.json" \
  --queues "${validation_tmp_dir}/platform__kueue.json"

note "checking GMP selectors, ports, and Prometheus recording rules"

observability_render="${validation_tmp_dir}/platform__observability.yaml"
control_plane_render="${validation_tmp_dir}/services__control-plane.yaml"
training_render="${validation_tmp_dir}/workloads__training__overlays__h100.yaml"
prometheus_rules="${validation_tmp_dir}/gmp-recording-rules.yaml"
prometheus_tests="${validation_tmp_dir}/promtool-tests.yaml"
[[ -s "${observability_render}" ]] || fail "platform/observability did not produce a render"
[[ -s "${control_plane_render}" ]] || fail "services/control-plane did not produce a render"
[[ -s "${training_render}" ]] || fail "H100 training profile did not produce a render"

pod_monitor_count="$(yq eval-all '[.] | flatten | map(select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring")) | length' \
  "${observability_render}")"
[[ "${pod_monitor_count}" == "2" ]] || fail "expected exactly two operator PodMonitoring resources"

control_plane_pod_monitor_count="$(yq eval-all '[.] | flatten | map(select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring")) | length' \
  "${control_plane_render}")"
[[ "${control_plane_pod_monitor_count}" == "1" ]] ||
  fail "services/control-plane must contain exactly one PodMonitoring resource"
control_admission_monitor_count="$(yq eval-all '[.] | flatten | map(select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring" and
  .metadata.name == "control-admission" and .metadata.namespace == "mindclade-system")) | length' \
  "${control_plane_render}")"
[[ "${control_admission_monitor_count}" == "1" ]] ||
  fail "expected exactly one namespaced control-admission PodMonitoring resource"

pod_monitor_endpoint_value() {
  local monitor_name="$1"
  local value_path="$2"
  yq eval-all -r \
    "select(.apiVersion == \"monitoring.googleapis.com/v1\" and
      .kind == \"PodMonitoring\" and .metadata.name == \"${monitor_name}\") |
      .spec.endpoints[0].${value_path}" \
    "${observability_render}"
}

for monitor_name in kueue-controller jobset-controller; do
  endpoint_count="$(yq eval-all "[select(
    .apiVersion == \"monitoring.googleapis.com/v1\" and .kind == \"PodMonitoring\" and
    .metadata.name == \"${monitor_name}\") | .spec.endpoints[]] | length" \
    "${observability_render}")"
  [[ "${endpoint_count}" == "1" ]] || fail "${monitor_name} must define exactly one scrape endpoint"
  [[ "$(pod_monitor_endpoint_value "${monitor_name}" authorization.type)" == "Bearer" ]] ||
    fail "${monitor_name} must use bearer authorization"
  [[ "$(pod_monitor_endpoint_value "${monitor_name}" authorization.credentials.secret.key)" == "token" ]] ||
    fail "${monitor_name} must obtain its bearer credential from the token Secret key"
  [[ "$(pod_monitor_endpoint_value "${monitor_name}" tls.minVersion)" == "TLS12" ]] ||
    fail "${monitor_name} must require TLS 1.2 or newer"
  [[ "$(pod_monitor_endpoint_value "${monitor_name}" tls.ca.secret.key)" == "ca.crt" ]] ||
    fail "${monitor_name} must use a CA-only trust contract"
  [[ "$(pod_monitor_endpoint_value "${monitor_name}" tls.insecureSkipVerify)" != "true" ]] ||
    fail "${monitor_name} may not disable TLS verification"
done

[[ "$(pod_monitor_endpoint_value kueue-controller authorization.credentials.secret.name)" == "mindclade-gmp-kueue-auth" ]] ||
  fail "Kueue bearer-token contract drifted"
[[ "$(pod_monitor_endpoint_value kueue-controller tls.ca.secret.name)" == "mindclade-gmp-kueue-trust" ]] ||
  fail "Kueue CA-only trust contract drifted"
[[ "$(pod_monitor_endpoint_value kueue-controller tls.cert.secret.name)" == "null" &&
  "$(pod_monitor_endpoint_value kueue-controller tls.key.secret.name)" == "null" ]] ||
  fail "Kueue must not receive an unnecessary client private key"

[[ "$(pod_monitor_endpoint_value jobset-controller authorization.credentials.secret.name)" == "mindclade-gmp-jobset-auth" ]] ||
  fail "JobSet bearer-token contract drifted"
[[ "$(pod_monitor_endpoint_value jobset-controller tls.ca.secret.name)" == "mindclade-gmp-jobset-trust" ]] ||
  fail "JobSet CA-only trust contract drifted"
[[ "$(pod_monitor_endpoint_value jobset-controller tls.cert.secret.name)" == "mindclade-gmp-jobset-client" &&
  "$(pod_monitor_endpoint_value jobset-controller tls.cert.secret.key)" == "tls.crt" &&
  "$(pod_monitor_endpoint_value jobset-controller tls.key.secret.name)" == "mindclade-gmp-jobset-client" &&
  "$(pod_monitor_endpoint_value jobset-controller tls.key.secret.key)" == "tls.key" ]] ||
  fail "JobSet client-auth certificate contract drifted"

serving_secret_reference_count="$(yq eval-all '[select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring") |
  .spec.endpoints[] | .. | select(tag == "!!str" and
    (. == "kueue-metrics-server-cert" or . == "jobset-metrics-server-cert"))] | length' \
  "${observability_render}")"
[[ "${serving_secret_reference_count}" == "0" ]] ||
  fail "collectors may not read controller serving Secrets that contain private keys"

jobset_labeldrop_count="$(yq eval-all '[select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring" and
  .metadata.name == "jobset-controller") | .spec.endpoints[].metricRelabeling[]? |
  select(.action == "labeldrop")] | length' "${observability_render}")"
[[ "${jobset_labeldrop_count}" == "0" ]] ||
  fail "JobSet labels may be removed only after aggregation; scrape-time labeldrop corrupts counters"

jobset_metric_keep_regex="$(pod_monitor_endpoint_value jobset-controller \
  'metricRelabeling[] | select(.action == "keep") | .regex')"
[[ "${jobset_metric_keep_regex}" != *"jobset_failed"* &&
  "${jobset_metric_keep_regex}" != *"jobset_completed"* ]] ||
  fail "unreliable per-name JobSet terminal counters may not drive windowed outcome alerts"

control_admission_endpoint_value() {
  local value_path="$1"
  yq eval-all -r \
    "select(.apiVersion == \"monitoring.googleapis.com/v1\" and
      .kind == \"PodMonitoring\" and .metadata.name == \"control-admission\") |
      .spec.endpoints[0].${value_path}" \
    "${control_plane_render}"
}

control_admission_endpoint_count="$(yq eval-all '[select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring" and
  .metadata.name == "control-admission") | .spec.endpoints[]] | length' \
  "${control_plane_render}")"
[[ "${control_admission_endpoint_count}" == "1" ]] ||
  fail "control-admission must define exactly one scrape endpoint"
[[ "$(control_admission_endpoint_value port)" == "metrics" &&
  "$(control_admission_endpoint_value scheme)" == "http" &&
  "$(control_admission_endpoint_value path)" == "/metrics" &&
  "$(control_admission_endpoint_value interval)" == "30s" &&
  "$(control_admission_endpoint_value timeout)" == "10s" ]] ||
  fail "control-admission scrape endpoint contract drifted"
[[ "$(control_admission_endpoint_value authorization)" == "null" &&
  "$(control_admission_endpoint_value tls)" == "null" ]] ||
  fail "control-admission must rely on exact collector NetworkPolicy identity, not guessed scrape credentials"

control_admission_selector="$(yq eval-all -r 'select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring" and
  .metadata.name == "control-admission") |
  .spec.selector.matchLabels."observability.mindclade.dev/control-admission"' \
  "${control_plane_render}")"
[[ "${control_admission_selector}" == "true" ]] ||
  fail "control-admission PodMonitoring selector must use the dedicated bounded telemetry label"
[[ "$(control_admission_endpoint_value 'metricRelabeling[] | select(.action == "keep") | .regex')" == \
  '^(up|mindclade_control_admission_(decisions_total|decision_duration_seconds_(bucket|count|sum)|expiration_backlog|oldest_expired_reservation_age_seconds|last_successful_sweep_timestamp_seconds|consecutive_backlogged_sweeps|event_drift|snapshot_last_success_timestamp_seconds|snapshot_success))$' ]] ||
  fail "control-admission scrape allowlist drifted"

control_admission_role_source="$(yq eval-all -r 'select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring" and
  .metadata.name == "control-admission") | .spec.targetLabels.fromPod[0].from' \
  "${control_plane_render}")"
control_admission_role_target="$(yq eval-all -r 'select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring" and
  .metadata.name == "control-admission") | .spec.targetLabels.fromPod[0].to' \
  "${control_plane_render}")"
control_admission_service_source="$(yq eval-all -r 'select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring" and
  .metadata.name == "control-admission") | .spec.targetLabels.fromPod[1].from' \
  "${control_plane_render}")"
control_admission_service_target="$(yq eval-all -r 'select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring" and
  .metadata.name == "control-admission") | .spec.targetLabels.fromPod[1].to' \
  "${control_plane_render}")"
[[ "${control_admission_role_source}" == "mindclade.dev/control-plane-role" &&
  "${control_admission_role_target}" == "role" &&
  "${control_admission_service_source}" == "observability.mindclade.dev/service" &&
  "${control_admission_service_target}" == "service" ]] ||
  fail "control-admission must map the bounded process role and fixed service target labels"
control_admission_target_label_count="$(yq eval-all '[select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring" and
  .metadata.name == "control-admission") | .spec.targetLabels.fromPod[]] | length' \
  "${control_plane_render}")"
[[ "${control_admission_target_label_count}" == "2" ]] ||
  fail "control-admission must expose exactly the role and service target labels"

control_admission_metrics_address="$(yq eval-all -r 'select(
  .kind == "ConfigMap" and .metadata.name == "control-plane-runtime-config") |
  .data.MINDCLADE_METRICS_ADDRESS' "${control_plane_render}")"
[[ "${control_admission_metrics_address}" == "0.0.0.0:9464" ]] ||
  fail "control-plane metrics listener must bind the declared TCP 9464 port"
control_admission_deployment_count="$(yq eval-all '[.] | flatten | map(select(
  .kind == "Deployment" and
  .spec.template.metadata.labels."observability.mindclade.dev/control-admission" == "true" and
  .spec.template.metadata.labels."observability.mindclade.dev/service" == "control-admission")) | length' \
  "${control_plane_render}")"
[[ "${control_admission_deployment_count}" == "2" ]] ||
  fail "exactly the API and maintenance Deployments must declare control-admission telemetry"
control_admission_deployment_names="$(yq eval-all -r '[.] | flatten | map(select(
  .kind == "Deployment" and
  .spec.template.metadata.labels."observability.mindclade.dev/control-admission" == "true" and
  .spec.template.metadata.labels."observability.mindclade.dev/service" == "control-admission")) |
  map(.metadata.name) | sort | join(",")' "${control_plane_render}")"
[[ "${control_admission_deployment_names}" == "control-plane-api,control-plane-maintenance" ]] ||
  fail "control-admission telemetry selectors may belong only to API and maintenance"
control_admission_metrics_port_violation_count="$(yq eval-all '[.] | flatten |
  map(select(.kind == "Deployment" and
    .spec.template.metadata.labels."observability.mindclade.dev/control-admission" == "true")) |
  map(select(([.spec.template.spec.containers[].ports[]? |
    select(.name == "metrics" and .containerPort == 9464 and .protocol == "TCP")] | length) != 1)) |
  length' "${control_plane_render}")"
[[ "${control_admission_metrics_port_violation_count}" == "0" ]] ||
  fail "every selected control-admission Pod must expose exactly one named TCP metrics port"
control_admission_metrics_port_owner_count="$(yq eval-all '[.] | flatten |
  map(select(.kind == "Deployment")) |
  map(select([.spec.template.spec.containers[].ports[]? |
    select(.name == "metrics" or .containerPort == 9464)] | length > 0)) |
  length' "${control_plane_render}")"
[[ "${control_admission_metrics_port_owner_count}" == "2" ]] ||
  fail "only the source-owned API and maintenance listeners may expose a control-admission metrics port"

control_admission_base_metrics_ingress_count="$(yq eval-all '[.] | flatten |
  [ .[] | select(.kind == "NetworkPolicy") | .spec.ingress[]?.ports[]? |
    select(.port == 9464 or .port == "metrics") ] | length' "${control_plane_render}")"
[[ "${control_admission_base_metrics_ingress_count}" == "0" ]] ||
  fail "base control-plane policy may not guess a GMP collector identity or allow metrics ingress"
control_admission_activation_blocker="$(yq eval-all -r 'select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "PodMonitoring" and
  .metadata.name == "control-admission") | .metadata.annotations."mindclade.dev/activation-blocker"' \
  "${control_plane_render}")"
[[ "${control_admission_activation_blocker}" == \
  "exact-gmp-collector-network-identity-and-connected-scrape-unqualified" ]] ||
  fail "control-admission PodMonitoring must remain activation-blocked on collector identity and connected evidence"
control_admission_api_rules_blocker="$(yq eval-all -r 'select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "Rules" and
  .metadata.name == "control-admission-api-recording") |
  .metadata.annotations."mindclade.dev/activation-blocker"' "${control_plane_render}")"
[[ "${control_admission_api_rules_blocker}" == \
  "connected-gmp-rule-and-alert-translation-qualification-missing" ]] ||
  fail "control-admission API rules must remain blocked on connected rule and alert translation evidence"
control_admission_api_metrics_contract="$(yq eval-all -r 'select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "Rules" and
  .metadata.name == "control-admission-api-recording") |
  .metadata.annotations."mindclade.dev/metrics-contract"' "${control_plane_render}")"
[[ "${control_admission_api_metrics_contract}" == \
  "admission-api-v2:60-decision-counters:72-histogram-buckets:6-histogram-counts:6-histogram-sums-per-replica" ]] ||
  fail "control-admission API rules must pin the exact v2 per-replica metric inventory"
control_admission_maintenance_rules_blocker="$(yq eval-all -r 'select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "Rules" and
  .metadata.name == "control-admission-maintenance-recording") |
  .metadata.annotations."mindclade.dev/activation-blocker"' "${control_plane_render}")"
[[ "${control_admission_maintenance_rules_blocker}" == \
  "connected-gmp-rule-alert-and-representative-postgresql-qualification-missing" ]] ||
  fail "control-admission maintenance rules must remain blocked on connected database, GMP, and alert evidence"
control_admission_maintenance_metrics_contract="$(yq eval-all -r 'select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "Rules" and
  .metadata.name == "control-admission-maintenance-recording") |
  .metadata.annotations."mindclade.dev/metrics-contract"' "${control_plane_render}")"
[[ "${control_admission_maintenance_metrics_contract}" == \
  "admission-maintenance-v1:four-scalars:three-drift-kinds:two-snapshot-timestamps:two-snapshot-outcomes-per-replica" ]] ||
  fail "control-admission maintenance rules must pin the exact v1 per-replica metric inventory"
control_admission_rules_document_count="$(yq eval-all '[.] | flatten | map(select(
  .apiVersion == "monitoring.googleapis.com/v1" and .kind == "Rules")) | length' \
  "${control_plane_render}")"
[[ "${control_admission_rules_document_count}" == "2" ]] ||
  fail "control-plane must keep API and activation-blocked maintenance rules in exactly two resources"

yq eval-all -o=yaml -I=2 \
  '[.] | flatten |
  map(select(.apiVersion == "monitoring.googleapis.com/v1" and .kind == "Rules")) |
  map(.spec.groups[]) | {"groups": .}' \
  "${observability_render}" "${control_plane_render}" "${training_render}" >"${prometheus_rules}"
rules_document_count="$(yq eval-all '[.] | flatten | map(select(.kind == "Rules")) | length' \
  "${observability_render}" "${control_plane_render}" "${training_render}")"
rules_group_count="$(yq eval '.groups | length' "${prometheus_rules}")"
[[ "${rules_document_count}" == "5" && "${rules_group_count}" == "5" ]] ||
  fail "expected all five GMP Rules documents to contribute exactly one Prometheus group"
promtool check rules "${prometheus_rules}"
sed "s|__RULE_FILE__|${prometheus_rules}|g" "${script_dir}/promtool-tests.yaml" >"${prometheus_tests}"
promtool test rules "${prometheus_tests}"

note "Kubernetes validation passed"
printf 'Validated %s built-in rendered resources, %s operator Helm chart(s), %s application Helm chart(s), and every declared Kustomize root.\n' \
  "${core_resource_count}" "${chart_count}" "${application_chart_count}"
