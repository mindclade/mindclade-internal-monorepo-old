#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

set -euo pipefail
IFS=$'\n\t'

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "${script_dir}/yamllint.yaml" ]]; then
  validation_support_dir="${script_dir}"
  gitops_root="$(cd -- "${script_dir}/.." && pwd)"
elif [[ -f "${script_dir}/tests/yamllint.yaml" ]]; then
  # A Bazel sh_test executable is placed at the package root while its data keeps source paths.
  validation_support_dir="${script_dir}/tests"
  gitops_root="${script_dir}"
else
  printf 'ERROR: cannot locate GitOps validation support files from %s\n' "${script_dir}" >&2
  exit 1
fi
repository_root="$(cd -- "${gitops_root}/../.." && pwd)"
canonical_repo="https://github.com/mindclade-org/mindclade.git"
revision_placeholder="SET_EXACT_40_CHAR_COMMIT_SHA"

# Developer invocations and CI share one Bazel-owned implementation. Until every CLI is a native
# Bazel toolchain, Nix supplies the pinned closure that the declared-input bridge captures.
if [[ "${MINDCLADE_VALIDATION_INTERNAL:-}" != "1" ]]; then
  exec nix develop "${repository_root}#ci" --command "${repository_root}/tools/dev/bazelw" test \
    //infra/gitops:validate --test_output=errors
fi

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

note() {
  printf '\n==> %s\n' "$*"
}

require_tool() {
  local tool_name="$1"
  command -v "${tool_name}" >/dev/null 2>&1 ||
    fail "required tool '${tool_name}' is missing; enter the pinned validation environment"
}

for tool_name in conftest diff find grep helm kubeconform kustomize mkdir mktemp python3 sed sort yamllint yq; do
  require_tool "${tool_name}"
done

[[ -d "${MINDCLADE_KUBERNETES_SCHEMA_DIR:-}" ]] ||
  fail "declared local Kubernetes schema directory is missing"
[[ -f "${MINDCLADE_CERT_MANAGER_SOURCE:-}" ]] ||
  fail "declared cert-manager fixed-output source is missing"
[[ -f "${MINDCLADE_TOOLCHAIN_MANIFEST:-}" ]] ||
  fail "declared Nix toolchain manifest is missing"

yq_version="$(yq --version 2>&1)"
[[ "${yq_version}" == *"mikefarah/yq"* ]] ||
  fail "mikefarah/yq v4 is required; found ${yq_version}"

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
verify_tool_version yamllint '.ciTools.yamllint' "$(yamllint --version 2>&1)"
verify_tool_version yq '.ciTools.yq' "${yq_version}"

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

validation_tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/mindclade-gitops-validation.XXXXXX")"
cleanup() {
  rm -rf -- "${validation_tmp_dir}"
}
trap cleanup EXIT

note "parsing owned YAML and rejecting stale or credential-bearing contracts"
while IFS= read -r -d '' yaml_file; do
  yq eval-all '.' "${yaml_file}" >/dev/null || fail "invalid YAML: ${yaml_file}"
done < <(find "${gitops_root}" -type f \( -name '*.yaml' -o -name '*.yml' \) -print0)

while IFS= read -r -d '' yaml_file; do
  case "${yaml_file}" in
    */vendor/cert-manager/v1.19.1/crds/upstream.yaml | \
      */vendor/cert-manager/v1.19.1/controllers/upstream.yaml)
      # Exact generated upstream artifacts are digest and inventory checked below.
      continue
      ;;
  esac
  yamllint --strict --config-file "${validation_support_dir}/yamllint.yaml" "${yaml_file}"
done < <(find "${gitops_root}" -type f \( -name '*.yaml' -o -name '*.yml' \) -print0)

if grep -R -n -E 'gitops/scripts/render\.sh|\./scripts/render\.sh' "${gitops_root}" >/dev/null; then
  fail "stale reference to the nonexistent render.sh workflow"
fi

if grep -R -n -E --include='*.yaml' --include='*.yml' \
  '^kind:[[:space:]]+Secret$' "${gitops_root}" >/dev/null; then
  fail "GitOps source must not author Secret objects"
fi

note "rendering every GitOps Kustomize root with default load restrictions"
while IFS= read -r kustomization_file; do
  root_dir="${kustomization_file%/kustomization.yaml}"
  root_name="${root_dir#"${gitops_root}/"}"
  output_name="${root_name//\//__}"
  output_file="${validation_tmp_dir}/${output_name}.yaml"
  kustomize build --load-restrictor LoadRestrictionsRootOnly "${root_dir}" >"${output_file}" ||
    fail "Kustomize render failed: ${root_name}"
  resource_count="$({
    yq eval-all '[.] | flatten | map(select(.kind != null and .apiVersion != null)) | length' \
      "${output_file}"
  })"
  [[ "${resource_count}" -gt 0 ]] || fail "Kustomize root rendered zero objects: ${root_name}"
  printf 'RENDERED %-55s %s resources\n' "${root_name}" "${resource_count}"
done < <(find "${gitops_root}" -type f -name kustomization.yaml -print | LC_ALL=C sort)

validate_paused_applications() {
  local manifest_file="$1"
  local expected_count="$2"
  local label="$3"
  local application_count
  local violation_count

  application_count="$({
    yq eval-all '[select(.kind == "Application")] | length' "${manifest_file}"
  })"
  [[ "${application_count}" == "${expected_count}" ]] ||
    fail "${label}: expected ${expected_count} Applications, found ${application_count}"

  violation_count="$({
    MINDCLADE_VALIDATION_REPO="${canonical_repo}" \
      MINDCLADE_VALIDATION_REVISION="${revision_placeholder}" \
      yq eval-all '
      [select(.kind == "Application") |
        select(
          .metadata.namespace != "argocd" or
          .metadata.annotations."argocd.argoproj.io/skip-reconcile" != "true" or
          .metadata.annotations."mindclade.dev/activation-state" != "blocked" or
          .spec.source.repoURL != strenv(MINDCLADE_VALIDATION_REPO) or
          .spec.source.targetRevision != strenv(MINDCLADE_VALIDATION_REVISION) or
          .spec.destination.server != "https://kubernetes.default.svc" or
          .spec.syncPolicy.automated.enabled != false or
          .spec.syncPolicy.automated.prune != false or
          .spec.syncPolicy.automated.selfHeal != false or
          ((.metadata.finalizers // []) | length) != 0
        )
      ] | length
    ' "${manifest_file}"
  })"
  [[ "${violation_count}" == "0" ]] ||
    fail "${label}: Application pause, revision, source, destination, or sync policy drifted"
}

note "checking the manually instantiated root application"
validate_paused_applications "${gitops_root}/argocd/app-of-apps.yaml" 1 "root application"
[[ "$(yq eval -r '.spec.source.path' "${gitops_root}/argocd/app-of-apps.yaml")" == \
  "infra/gitops/bootstrap/SET_ENVIRONMENT" ]] ||
  fail "root application must require an explicit single-environment bootstrap root"
[[ "$(yq eval -r '.spec.project' "${gitops_root}/argocd/app-of-apps.yaml")" == \
  "mindclade-bootstrap" ]] || fail "root application project drifted"

note "checking AppProject least privilege and governance separation"
projects_file="${gitops_root}/argocd/projects.yaml"
project_count="$(yq eval-all '[select(.kind == "AppProject")] | length' "${projects_file}")"
[[ "${project_count}" == "9" ]] || fail "expected nine scoped AppProjects, found ${project_count}"

expected_projects="${validation_tmp_dir}/expected-projects.txt"
actual_projects="${validation_tmp_dir}/actual-projects.txt"
printf '%s\n' \
  mindclade-bootstrap \
  mindclade-cert-manager-controller \
  mindclade-foundation \
  mindclade-jobset-controller \
  mindclade-kueue-controller \
  mindclade-ml-resources \
  mindclade-operator-crds \
  mindclade-operator-foundation \
  mindclade-operator-observability >"${expected_projects}"
yq eval-all -N -r 'select(.kind == "AppProject") | .metadata.name' "${projects_file}" |
  LC_ALL=C sort >"${actual_projects}"
diff -u "${expected_projects}" "${actual_projects}" >/dev/null ||
  fail "AppProject ownership boundaries drifted"

assert_project_destinations() {
  local project_name="$1"
  local expected_json="$2"
  local actual_json
  actual_json="$({
    MINDCLADE_VALIDATION_PROJECT="${project_name}" yq eval-all -o=json -I=0 '
      select(.kind == "AppProject" and
        .metadata.name == strenv(MINDCLADE_VALIDATION_PROJECT)) |
      [.spec.destinations[].namespace] | sort
    ' "${projects_file}"
  })"
  [[ "${actual_json}" == "${expected_json}" ]] ||
    fail "${project_name}: exact destination set drifted: ${actual_json}"
}

assert_project_destinations mindclade-bootstrap '["argocd"]'
assert_project_destinations mindclade-foundation \
  '["mindclade-batch-cpu","mindclade-system","mindclade-training-h100","mindclade-training-h200"]'
assert_project_destinations mindclade-operator-foundation \
  '["cert-manager","jobset-system","kueue-system"]'
assert_project_destinations mindclade-operator-crds \
  '["cert-manager","jobset-system","kueue-system"]'
assert_project_destinations mindclade-cert-manager-controller '["cert-manager","kube-system"]'
assert_project_destinations mindclade-jobset-controller '["jobset-system"]'
assert_project_destinations mindclade-kueue-controller '["kueue-system"]'
assert_project_destinations mindclade-operator-observability \
  '["jobset-system","kueue-system"]'
assert_project_destinations mindclade-ml-resources \
  '["mindclade-batch-cpu","mindclade-system","mindclade-training-h100","mindclade-training-h200"]'

[[ "$(yq eval-all '[select(.kind == "AppProject" and
  .metadata.name == "mindclade-operator-crds") |
  select((.spec.clusterResourceWhitelist | length) == 1 and
    .spec.clusterResourceWhitelist[0].group == "apiextensions.k8s.io" and
    .spec.clusterResourceWhitelist[0].kind == "CustomResourceDefinition" and
    (.spec.namespaceResourceWhitelist | length) == 0)] | length' "${projects_file}")" == "1" ]] ||
  fail "CRD AppProject may permit only CustomResourceDefinition"
[[ "$(yq eval-all '[select(.kind == "AppProject" and
  .metadata.name == "mindclade-operator-observability") |
  (.spec.namespaceResourceWhitelist // [])[] |
  select(.group == "monitoring.googleapis.com" and
    (.kind == "PodMonitoring" or .kind == "Rules"))] | length' "${projects_file}")" == "2" ]] ||
  fail "operator observability AppProject exact-kind permissions drifted"
[[ "$(yq eval-all '[select(.kind == "AppProject" and
  .metadata.name == "mindclade-ml-resources") |
  (.spec.clusterResourceWhitelist // [])[] |
  select(.group == "kueue.x-k8s.io" and .kind == "Topology")] | length' \
  "${projects_file}")" == "1" ]] ||
  fail "ML resource AppProject must permit the locked Kueue Topology"

project_violation_count="$({
  MINDCLADE_VALIDATION_REPO="${canonical_repo}" yq eval-all '
    [select(.kind == "AppProject") |
      select(
        .metadata.namespace != "argocd" or
        (.spec.sourceRepos | length) != 1 or
        .spec.sourceRepos[0] != strenv(MINDCLADE_VALIDATION_REPO) or
        .spec.orphanedResources.warn != true or
        (.spec.destinations | length) == 0 or
        ([.spec.destinations[] |
          select(.server != "https://kubernetes.default.svc")] | length) != 0
      )
    ] | length
  ' "${projects_file}"
})"
[[ "${project_violation_count}" == "0" ]] ||
  fail "AppProject source, destination, namespace, or orphan policy drifted"

wildcard_count="$({
  yq eval-all '
    [select(.kind == "AppProject") |
      ((.spec.clusterResourceWhitelist // []) + (.spec.namespaceResourceWhitelist // []))[] |
      select((.group | test("^\\*$")) or (.kind | test("^\\*$")))
    ] | length
  ' "${projects_file}"
})"
[[ "${wildcard_count}" == "0" ]] || fail "wildcard resource permission found in AppProject"

secret_permission_count="$({
  yq eval-all '
    [select(.kind == "AppProject") | (.spec.namespaceResourceWhitelist // [])[] |
      select(.group == "" and .kind == "Secret")
    ] | length
  ' "${projects_file}"
})"
[[ "${secret_permission_count}" == "0" ]] || fail "AppProject permits repository-managed Secrets"

self_escalation_count="$({
  yq eval-all '
    [select(.kind == "AppProject" and .metadata.name == "mindclade-bootstrap") |
      (.spec.namespaceResourceWhitelist // [])[] |
      select(.group == "argoproj.io" and .kind == "AppProject")
    ] | length
  ' "${projects_file}"
})"
[[ "${self_escalation_count}" == "0" ]] ||
  fail "bootstrap application may not rewrite its own AppProject"

note "checking non-secret repository and upstream content locks"
repository_contract="${gitops_root}/argocd/repositories.yaml"
[[ "$(yq eval -r '.kind' "${repository_contract}")" == "ConfigMap" ]] ||
  fail "repository contract must remain a non-operative ConfigMap"
[[ "$(yq eval -r '.data."repository.url"' "${repository_contract}")" == "${canonical_repo}" ]] ||
  fail "repository contract URL drifted"
[[ "$(yq eval -r '.data."repository.registration-state"' "${repository_contract}")" == "absent" ]] ||
  fail "source repository registration must remain external"

lock_file="${gitops_root}/argocd/bootstrap/argocd.lock.yaml"
[[ "$(yq eval -r '.kind' "${lock_file}")" == "ConfigMap" ]] ||
  fail "upstream content lock must be a standard ConfigMap"
locked_sha="$(yq eval -r '.data."cert-manager.sha256"' "${lock_file}")"
locked_bytes="$(yq eval -r '.data."cert-manager.bytes"' "${lock_file}")"
locked_version="$(yq eval -r '.data."cert-manager.version"' "${lock_file}")"
locked_url="$(yq eval -r '.data."cert-manager.url"' "${lock_file}")"
[[ "${locked_sha}" =~ ^[0-9a-f]{64}$ ]] || fail "cert-manager lock has an invalid SHA-256"
[[ "${locked_bytes}" =~ ^[1-9][0-9]*$ ]] || fail "cert-manager lock has an invalid byte count"
[[ "${locked_url}" == *"/${locked_version}/cert-manager.yaml" ]] ||
  fail "cert-manager URL and version lock disagree"
[[ "$(sha256_file "${MINDCLADE_CERT_MANAGER_SOURCE}")" == "${locked_sha}" ]] ||
  fail "cert-manager fixed-output source SHA-256 disagrees with the content lock"
[[ "$(wc -c <"${MINDCLADE_CERT_MANAGER_SOURCE}" | tr -d ' ')" == "${locked_bytes}" ]] ||
  fail "cert-manager fixed-output source byte count disagrees with the content lock"

cert_manager_split_files=()
for phase in crds controllers; do
  phase_path="$(yq eval -r ".data.\"cert-manager.${phase}.path\"" "${lock_file}")"
  phase_sha="$(yq eval -r ".data.\"cert-manager.${phase}.sha256\"" "${lock_file}")"
  phase_bytes="$(yq eval -r ".data.\"cert-manager.${phase}.bytes\"" "${lock_file}")"
  phase_objects="$(yq eval -r ".data.\"cert-manager.${phase}.objects\"" "${lock_file}")"
  phase_file="${repository_root}/${phase_path}"
  [[ -f "${phase_file}" ]] || fail "cert-manager ${phase} artifact is missing: ${phase_path}"
  [[ "$(sha256_file "${phase_file}")" == "${phase_sha}" ]] ||
    fail "cert-manager ${phase} generated artifact SHA-256 drifted"
  [[ "$(wc -c <"${phase_file}" | tr -d ' ')" == "${phase_bytes}" ]] ||
    fail "cert-manager ${phase} generated artifact byte count drifted"
  [[ "$(yq eval-all '[select(.kind != null and .apiVersion != null)] | length' \
    "${phase_file}")" == "${phase_objects}" ]] ||
    fail "cert-manager ${phase} object count drifted"
  cert_manager_split_files+=("${phase_file}")
done

cert_manager_crd_file="${cert_manager_split_files[0]}"
cert_manager_controller_file="${cert_manager_split_files[1]}"
[[ "$(yq eval-all '[select(.kind != "CustomResourceDefinition" and .kind != null)] | length' \
  "${cert_manager_crd_file}")" == "0" ]] || fail "cert-manager CRD phase contains non-CRDs"
[[ "$(yq eval-all '[select(.kind == "CustomResourceDefinition")] | length' \
  "${cert_manager_controller_file}")" == "0" ]] || fail "cert-manager controller phase contains CRDs"

python3 "${gitops_root}/vendor/cert-manager/split_release.py" \
  --source "${MINDCLADE_CERT_MANAGER_SOURCE}" \
  --expected-sha256 "${locked_sha}" \
  --expected-bytes "${locked_bytes}" \
  --expected-objects "$(yq eval -r '.data."cert-manager.objects"' "${lock_file}")" \
  --expected-crds "$(yq eval -r '.data."cert-manager.crds.objects"' "${lock_file}")" \
  --crds-output "${cert_manager_crd_file}" \
  --controllers-output "${cert_manager_controller_file}" \
  --check

cert_manager_union_json="${validation_tmp_dir}/cert-manager-union.json"
cert_manager_source_json="${validation_tmp_dir}/cert-manager-source.json"
yq eval-all -o=json -I=0 '
  [.] | flatten | map(select(.kind != null and .apiVersion != null)) |
  sort_by(.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name)
' "${cert_manager_crd_file}" "${cert_manager_controller_file}" >"${cert_manager_union_json}"
yq eval-all -o=json -I=0 '
  [.] | flatten | map(select(.kind != null and .apiVersion != null)) |
  sort_by(.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name)
' "${MINDCLADE_CERT_MANAGER_SOURCE}" >"${cert_manager_source_json}"
diff -u "${cert_manager_source_json}" "${cert_manager_union_json}" >/dev/null ||
  fail "cert-manager generated split does not normalize to the fixed-output source"
[[ "$(sha256_file "${cert_manager_union_json}")" == \
  "$(yq eval -r '.data."cert-manager.normalized-sha256"' "${lock_file}")" ]] ||
  fail "cert-manager split normalized union no longer matches the locked upstream inventory"
[[ "$(yq eval -r 'length' "${cert_manager_union_json}")" == \
  "$(yq eval -r '.data."cert-manager.objects"' "${lock_file}")" ]] ||
  fail "cert-manager split union does not contain all locked upstream objects"

cert_manager_crd_render="${validation_tmp_dir}/cert-manager-crds.yaml"
cert_manager_controller_render="${validation_tmp_dir}/cert-manager-controllers.yaml"
kustomize build --load-restrictor LoadRestrictionsRootOnly \
  "${gitops_root}/vendor/cert-manager/v1.19.1/crds" >"${cert_manager_crd_render}"
kustomize build --load-restrictor LoadRestrictionsRootOnly \
  "${gitops_root}/vendor/cert-manager/v1.19.1/controllers" >"${cert_manager_controller_render}"
[[ "$(yq eval-all '[select(.kind == "CustomResourceDefinition" and
  .metadata.annotations."argocd.argoproj.io/sync-options" == "Prune=false,Delete=false")] |
  length' "${cert_manager_crd_render}")" == "6" ]] ||
  fail "cert-manager CRDs lost no-prune/no-delete protection"
[[ "$(yq eval-all '[select(.kind == "Namespace")] | length' \
  "${cert_manager_controller_render}")" == "0" ]] ||
  fail "cert-manager controller phase must not share Namespace ownership"
[[ "$(yq eval-all '[select(.kind == "PodDisruptionBudget")] | length' \
  "${cert_manager_controller_render}")" == "3" ]] ||
  fail "cert-manager controller phase must render three availability budgets"
[[ "$(yq eval-all '[select(.kind == "Deployment" and (.spec.replicas // 0) < 2)] | length' \
  "${cert_manager_controller_render}")" == "0" ]] ||
  fail "cert-manager controller deployments must remain highly available"
[[ "$(yq eval-all '[select(.kind == "Deployment") |
  .spec.template.spec.containers[].image | select(test("@sha256:[0-9a-f]{64}$") == false)] |
  length' "${cert_manager_controller_render}")" == "0" ]] ||
  fail "cert-manager controller images must remain digest pinned"
[[ "$(yq eval-all '[select(.kind == "Deployment") |
  .spec.template.spec.containers[] | select(
    .resources.requests."ephemeral-storage" == null or
    .resources.limits."ephemeral-storage" == null)] | length' \
  "${cert_manager_controller_render}")" == "0" ]] ||
  fail "cert-manager controller containers require ephemeral-storage requests and limits"
[[ "$(yq eval -r '.data."argocd.installation-state"' "${lock_file}")" == blocked-* ]] ||
  fail "Argo CD bootstrap must stay blocked until the live repository supplies its chart lock"

for chart_lock_key in jobset kueue; do
  chart_lock_path="$(yq eval -r ".data.\"${chart_lock_key}.chart-lock\"" "${lock_file}")"
  [[ -f "${repository_root}/${chart_lock_path}" ]] ||
    fail "declared chart lock does not exist: ${chart_lock_path}"
done

note "checking fail-closed operator namespace ownership"
operator_foundation_dir="${repository_root}/infra/kubernetes/platform/operator-system/foundation"
operator_foundation="${operator_foundation_dir}/resources.yaml"
[[ ! -f "${operator_foundation_dir}/kustomization.yaml" ]] ||
  fail "operator foundation must remain an explicit plain-directory source"
[[ "$(find "${operator_foundation_dir}" -maxdepth 1 -type f \
  \( -name '*.yaml' -o -name '*.yml' \) | wc -l | tr -d ' ')" == "1" ]] ||
  fail "operator foundation plain directory must contain one unambiguous YAML source"
kubeconform -exit-on-error -kubernetes-version 1.36.2 \
  -schema-location "${MINDCLADE_KUBERNETES_SCHEMA_DIR}/{{.ResourceKind}}{{.KindSuffix}}.json" \
  -strict -summary "${operator_foundation}" >/dev/null
conftest test --no-color \
  --policy "${repository_root}/infra/kubernetes/tests/policy" \
  --rego-version v1 --strict "${operator_foundation}" >/dev/null
[[ "$(yq eval-all '[select(.kind == "Namespace")] | length' "${operator_foundation}")" == "3" ]] ||
  fail "operator foundation must own exactly three namespaces"
[[ "$(yq eval-all '[select(.kind == "NetworkPolicy" and
  (.spec.policyTypes | length) == 2 and
  (.spec.policyTypes | contains(["Ingress", "Egress"])) and
  ((.spec.ingress // []) | length) == 0 and ((.spec.egress // []) | length) == 0)] | length' \
  "${operator_foundation}")" == "3" ]] ||
  fail "operator foundation must default-deny ingress and egress in every namespace"
[[ "$(yq eval-all '[select(.kind == "Namespace" and
  .metadata.labels."mindclade.dev/admission" == "platform-operator" and
  .metadata.labels."mindclade.dev/workload-activation" == "platform-operator")] | length' \
  "${operator_foundation}")" == "3" ]] ||
  fail "operator namespaces must use the explicit operator admission class"
[[ "$(yq eval-all '[select(.kind == "Namespace" and (
  .metadata.labels."mindclade.dev/admission" == "enforced" or
  .metadata.labels."mindclade.dev/workload-activation" == "blocked"))] | length' \
  "${operator_foundation}")" == "0" ]] ||
  fail "operator controller namespaces may never select standard or blocked workload VAPs"

note "validating environment roots and exact Kubernetes-overlay parity"
for environment in development staging production; do
  gitops_environment_root="${gitops_root}/environments/${environment}"
  kubernetes_environment_root="${repository_root}/infra/kubernetes/overlays/${environment}"
  gitops_render="${validation_tmp_dir}/environment-${environment}.yaml"
  kubernetes_render="${validation_tmp_dir}/kubernetes-${environment}.yaml"
  normalized_gitops="${validation_tmp_dir}/environment-${environment}.json"
  normalized_kubernetes="${validation_tmp_dir}/kubernetes-${environment}.json"

  [[ "$(yq eval -o=json -I=0 '.resources' "${gitops_environment_root}/kustomization.yaml")" == \
    "[\"../../../kubernetes/overlays/${environment}\"]" ]] ||
    fail "${environment}: GitOps root must compose exactly its Kubernetes overlay"
  [[ "$(yq eval -r '.namespace // ""' "${gitops_environment_root}/kustomization.yaml")" == "" ]] ||
    fail "${environment}: top-level namespace transformer would corrupt cluster-scoped resources"
  [[ "$(yq eval -r '.configMapGenerator[0].namespace' \
    "${gitops_environment_root}/kustomization.yaml")" == "mindclade-system" ]] ||
    fail "${environment}: environment ConfigMap namespace drifted"

  kustomize build --load-restrictor LoadRestrictionsRootOnly "${gitops_environment_root}" \
    >"${gitops_render}"
  kustomize build --load-restrictor LoadRestrictionsRootOnly "${kubernetes_environment_root}" \
    >"${kubernetes_render}"

  kubeconform -exit-on-error -kubernetes-version 1.36.2 \
    -schema-location "${MINDCLADE_KUBERNETES_SCHEMA_DIR}/{{.ResourceKind}}{{.KindSuffix}}.json" \
    -strict -summary \
    "${gitops_render}" >/dev/null

  environment_config_count="$({
    MINDCLADE_VALIDATION_ENVIRONMENT="${environment}" yq eval-all '
      [select(
        .kind == "ConfigMap" and
        (.metadata.name | test("^mindclade-environment-")) and
        .metadata.namespace == "mindclade-system" and
        .data.MINDCLADE_ENV == strenv(MINDCLADE_VALIDATION_ENVIRONMENT) and
        .data.MINDCLADE_WORKLOAD_ACTIVATION == "blocked"
      )] | length
    ' "${gitops_render}"
  })"
  [[ "${environment_config_count}" == "1" ]] ||
    fail "${environment}: expected one blocked environment identity ConfigMap"

  pod_quota="$(yq eval-all -r \
    'select(.kind == "ResourceQuota" and .metadata.name == "mindclade-system") | .spec.hard.pods' \
    "${gitops_render}")"
  [[ "${pod_quota}" == "0" ]] || fail "${environment}: Pod quota must remain zero"

  cluster_namespace_count="$({
    yq eval-all '
      [select(
        .kind == "Namespace" or
        .kind == "PriorityClass" or
        .kind == "ValidatingAdmissionPolicy" or
        .kind == "ValidatingAdmissionPolicyBinding"
      ) | select(.metadata.namespace != null)] | length
    ' "${gitops_render}"
  })"
  [[ "${cluster_namespace_count}" == "0" ]] ||
    fail "${environment}: cluster-scoped resources acquired metadata.namespace"

  workload_count="$({
    yq eval-all '
      [select(
        .kind == "Pod" or .kind == "Deployment" or .kind == "StatefulSet" or
        .kind == "DaemonSet" or .kind == "Job" or .kind == "CronJob" or .kind == "JobSet"
      )] | length
    ' "${gitops_render}"
  })"
  [[ "${workload_count}" == "0" ]] ||
    fail "${environment}: foundation root unexpectedly contains a workload"

  yq eval-all -o=json -I=0 '.' "${kubernetes_render}" >"${normalized_kubernetes}"
  yq eval-all -o=json -I=0 '
    select(
      .kind != "ConfigMap" or
      ((.metadata.name // "") | test("^mindclade-environment-")) == false
    )
  ' "${gitops_render}" >"${normalized_gitops}"
  diff -u "${normalized_kubernetes}" "${normalized_gitops}" >/dev/null ||
    fail "${environment}: GitOps root changes more than the environment identity ConfigMap"

  bootstrap_render="${validation_tmp_dir}/bootstrap-${environment}.yaml"
  kustomize build --load-restrictor LoadRestrictionsRootOnly \
    "${gitops_root}/bootstrap/${environment}" >"${bootstrap_render}"
  validate_paused_applications "${bootstrap_render}" 11 "${environment} child applications"

  while IFS='|' read -r app_suffix app_wave app_project app_path app_namespace; do
    matching_application_count="$({
      MINDCLADE_VALIDATION_NAME="mindclade-${environment}-${app_suffix}" \
      MINDCLADE_VALIDATION_WAVE="${app_wave}" \
      MINDCLADE_VALIDATION_PROJECT="${app_project}" \
      MINDCLADE_VALIDATION_PATH="${app_path}" \
      MINDCLADE_VALIDATION_NAMESPACE="${app_namespace}" \
      yq eval-all '[select(.kind == "Application" and
        .metadata.name == strenv(MINDCLADE_VALIDATION_NAME) and
        .metadata.annotations."argocd.argoproj.io/sync-wave" ==
          strenv(MINDCLADE_VALIDATION_WAVE) and
        .spec.project == strenv(MINDCLADE_VALIDATION_PROJECT) and
        .spec.source.path == strenv(MINDCLADE_VALIDATION_PATH) and
        .spec.destination.namespace == strenv(MINDCLADE_VALIDATION_NAMESPACE))] | length' \
        "${bootstrap_render}"
    })"
    [[ "${matching_application_count}" == "1" ]] ||
      fail "${environment}: phase contract drifted for ${app_suffix}"
  done <<EOF
operator-foundation|-80|mindclade-operator-foundation|infra/kubernetes/platform/operator-system/foundation|cert-manager
cert-manager-crds|-70|mindclade-operator-crds|infra/gitops/vendor/cert-manager/v1.19.1/crds|cert-manager
cert-manager-controller|-60|mindclade-cert-manager-controller|infra/gitops/vendor/cert-manager/v1.19.1/controllers|cert-manager
jobset-crds|-50|mindclade-operator-crds|infra/kubernetes/platform/jobset/chart|jobset-system
jobset-controller|-40|mindclade-jobset-controller|infra/kubernetes/platform/jobset/chart|jobset-system
kueue-crds|-30|mindclade-operator-crds|infra/kubernetes/platform/kueue/chart|kueue-system
kueue-controller|-20|mindclade-kueue-controller|infra/kubernetes/platform/kueue/chart|kueue-system
operator-observability|-10|mindclade-operator-observability|infra/kubernetes/platform/observability|kueue-system
foundation|0|mindclade-foundation|infra/gitops/environments/${environment}|mindclade-system
kueue-resources|20|mindclade-ml-resources|infra/kubernetes/platform/kueue|mindclade-system
jobset-resources|22|mindclade-ml-resources|infra/kubernetes/platform/jobset|mindclade-system
EOF

  crd_application_violation_count="$({
    yq eval-all '[select(.kind == "Application" and
      (.metadata.name | test("-crds$"))) | select(
        .spec.project != "mindclade-operator-crds" or
        (.spec.syncPolicy.syncOptions | contains(["ServerSideApply=true"])) == false or
        (.spec.syncPolicy.syncOptions |
          contains(["DisableClientSideApplyMigration=true"])) == false or
        .spec.syncPolicy.automated.prune != false
      )] | length' "${bootstrap_render}"
  })"
  [[ "${crd_application_violation_count}" == "0" ]] ||
    fail "${environment}: CRD applications lost SSA or non-pruning lifecycle controls"

  [[ "$(yq eval-all '[select(.kind == "Application" and
    .spec.source.helm.releaseName == "jobset")] | length' "${bootstrap_render}")" == "2" ]] ||
    fail "${environment}: JobSet phases must use canonical release identity jobset"
  [[ "$(yq eval-all '[select(.kind == "Application" and
    .spec.source.helm.releaseName == "kueue")] | length' "${bootstrap_render}")" == "2" ]] ||
    fail "${environment}: Kueue phases must use canonical release identity kueue"

  exact_phase_control_count="$({
    MINDCLADE_VALIDATION_ENVIRONMENT="${environment}" yq eval-all '[select(
      .kind == "Application" and (
        (.metadata.name ==
          ("mindclade-" + strenv(MINDCLADE_VALIDATION_ENVIRONMENT) + "-cert-manager-crds") and
          .spec.source.helm == null) or
        (.metadata.name ==
          ("mindclade-" + strenv(MINDCLADE_VALIDATION_ENVIRONMENT) +
            "-cert-manager-controller") and .spec.source.helm == null) or
        (.metadata.name ==
          ("mindclade-" + strenv(MINDCLADE_VALIDATION_ENVIRONMENT) + "-jobset-crds") and
          .spec.source.helm.releaseName == "jobset" and
          .spec.source.helm.skipCrds == false and
          .spec.source.helm.valuesObject.jobset.controller.enabled == false) or
        (.metadata.name ==
          ("mindclade-" + strenv(MINDCLADE_VALIDATION_ENVIRONMENT) +
            "-jobset-controller") and .spec.source.helm.releaseName == "jobset" and
          .spec.source.helm.skipCrds == true and
          .spec.source.helm.valuesObject.jobset.controller.enabled == true) or
        (.metadata.name ==
          ("mindclade-" + strenv(MINDCLADE_VALIDATION_ENVIRONMENT) + "-kueue-crds") and
          .spec.source.helm.releaseName == "kueue" and
          .spec.source.helm.valuesObject.kueue.controller.enabled == false and
          .spec.source.helm.valuesObject.kueue.crds.enabled == true) or
        (.metadata.name ==
          ("mindclade-" + strenv(MINDCLADE_VALIDATION_ENVIRONMENT) +
            "-kueue-controller") and .spec.source.helm.releaseName == "kueue" and
          .spec.source.helm.valuesObject.kueue.controller.enabled == true and
          .spec.source.helm.valuesObject.kueue.crds.enabled == false)
      ))] | length' "${bootstrap_render}"
  })"
  [[ "${exact_phase_control_count}" == "6" ]] ||
    fail "${environment}: static/Helm phase controls drifted"
  [[ "$(yq eval-all '[select(.kind == "Application") |
    (.spec.syncPolicy.syncOptions // [])[] |
    select(. == "Force=true" or . == "Replace=true" or
      . == "SkipDryRunOnMissingResource=true" or . == "CreateNamespace=true")] | length' \
    "${bootstrap_render}")" == "0" ]] ||
    fail "${environment}: dangerous sync option found"

  broad_ignore_count="$({
    yq eval-all '[select(.kind == "Application") |
      (.spec.ignoreDifferences // [])[] | select(
        (.name // "") == "" or
        ((.managedFieldsManagers // []) | length) != 0 or
        ((.jsonPointers // []) | length) != 0 or
        ((.jqPathExpressions // []) | length) != 1 or
        ([.jqPathExpressions[] | select(
          . != ".webhooks[]?.clientConfig.caBundle" and
          . != ".spec.caBundle" and
          . != ".spec.conversion.webhook.clientConfig.caBundle")] | length) != 0
      )] | length' "${bootstrap_render}"
  })"
  [[ "${broad_ignore_count}" == "0" ]] ||
    fail "${environment}: ignoreDifferences must target one exact CA field on one exact object"

  other_environment_count="$({
    MINDCLADE_VALIDATION_ENVIRONMENT="${environment}" yq eval-all '
      [select(.kind == "Application") |
        select(
          .metadata.annotations."mindclade.dev/environment" !=
          strenv(MINDCLADE_VALIDATION_ENVIRONMENT)
        )
      ] | length
    ' "${bootstrap_render}"
  })"
  [[ "${other_environment_count}" == "0" ]] ||
    fail "${environment}: bootstrap root contains a different environment"

  printf 'VALIDATED %-51s foundation and eleven paused applications\n' "${environment}"
done

note "linting transactional operator chart phases"
for operator in kueue jobset; do
  chart_dir="${repository_root}/infra/kubernetes/platform/${operator}/chart"
  crd_render="${validation_tmp_dir}/${operator}-crds.yaml"
  controller_render="${validation_tmp_dir}/${operator}-controller.yaml"
  full_render="${validation_tmp_dir}/${operator}-full.yaml"
  phase_union="${validation_tmp_dir}/${operator}-phase-union.json"
  normalized_full="${validation_tmp_dir}/${operator}-full.json"
  [[ -f "${chart_dir}/Chart.lock" ]] || fail "${operator}: Chart.lock is missing"
  [[ -d "${chart_dir}/charts" ]] || fail "${operator}: vendored chart dependency is missing"
  helm lint --strict "${chart_dir}" >/dev/null
  if [[ "${operator}" == "kueue" ]]; then
    helm template kueue "${chart_dir}" --namespace kueue-system \
      --set kueue.controller.enabled=false --set kueue.crds.enabled=true >"${crd_render}"
    helm template kueue "${chart_dir}" --namespace kueue-system \
      --set kueue.controller.enabled=true --set kueue.crds.enabled=false >"${controller_render}"
    helm template kueue "${chart_dir}" --namespace kueue-system >"${full_render}"
    archive="${chart_dir}/charts/kueue-0.19.1.tgz"
    digest_key="MINDCLADE_KUEUE_VENDORED_CHART_ARCHIVE_SHA256"
  else
    helm template jobset "${chart_dir}" --namespace jobset-system --include-crds \
      --set jobset.controller.enabled=false >"${crd_render}"
    helm template jobset "${chart_dir}" --namespace jobset-system --skip-crds \
      --set jobset.controller.enabled=true >"${controller_render}"
    helm template jobset "${chart_dir}" --namespace jobset-system --include-crds >"${full_render}"
    archive="${chart_dir}/charts/jobset-0.12.0.tgz"
    digest_key="MINDCLADE_JOBSET_VENDORED_CHART_ARCHIVE_SHA256"
  fi

  expected_archive_digest="$(sed -n "s/^${digest_key}=sha256://p" \
    "${repository_root}/infra/kubernetes/versions.env")"
  [[ "$(sha256_file "${archive}")" == "${expected_archive_digest}" ]] ||
    fail "${operator}: vendored deterministic archive digest drifted"

  [[ "$(yq eval-all '[select(.kind != "CustomResourceDefinition" and .kind != null)] | length' \
    "${crd_render}")" == "0" ]] || fail "${operator}: CRD phase contains controller objects"
  [[ "$(yq eval-all '[select(.kind == "CustomResourceDefinition")] | length' \
    "${controller_render}")" == "0" ]] || fail "${operator}: controller phase contains CRDs"
  crd_count="$(yq eval-all '[select(.kind == "CustomResourceDefinition")] | length' \
    "${crd_render}")"
  controller_count="$(yq eval-all '[select(.kind == "Deployment")] | length' \
    "${controller_render}")"
  [[ "${crd_count}" -gt 0 && "${controller_count}" -gt 0 ]] ||
    fail "${operator}: both transactional phases must render objects"
  [[ "$(yq eval-all '[select(.kind == "CustomResourceDefinition" and
    .metadata.annotations."argocd.argoproj.io/sync-options" !=
      "Prune=false,Delete=false")] | length' "${crd_render}")" == "0" ]] ||
    fail "${operator}: a CRD lost no-prune/no-delete protection"

  yq eval-all -o=json -I=0 '
    [.] | flatten | map(select(.kind != null and .apiVersion != null)) |
    sort_by(.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name)
  ' "${crd_render}" "${controller_render}" >"${phase_union}"
  yq eval-all -o=json -I=0 '
    [.] | flatten | map(select(.kind != null and .apiVersion != null)) |
    sort_by(.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name)
  ' "${full_render}" >"${normalized_full}"
  diff -u "${normalized_full}" "${phase_union}" >/dev/null ||
    fail "${operator}: disjoint phase union does not equal the full chart render"

  authored_secret_count="$(yq eval-all '[select(.kind == "Secret")] | length' \
    "${controller_render}")"
  [[ "${authored_secret_count}" == "0" ]] ||
    fail "${operator}: cert-manager integration must not render a private-key Secret"
  certificate_count="$(yq eval-all '[select(.kind == "Certificate")] | length' \
    "${controller_render}")"
  [[ "${certificate_count}" -gt 0 ]] ||
    fail "${operator}: controller phase must render cert-manager Certificates"

  unpinned_image_count="$({
    yq eval-all '
      [select(.kind == "Deployment") | .spec.template.spec.containers[].image |
        select(test("@sha256:[0-9a-f]{64}$") == false)
      ] | length
    ' "${controller_render}"
  })"
  [[ "${unpinned_image_count}" == "0" ]] ||
    fail "${operator}: controller chart rendered a mutable container image"
  [[ "$(yq eval-all '[select(.kind == "Deployment") |
    .spec.template.spec.containers[] | select(
      .resources.requests."ephemeral-storage" == null or
      .resources.limits."ephemeral-storage" == null)] | length' \
    "${controller_render}")" == "0" ]] ||
    fail "${operator}: controller containers require ephemeral-storage requests and limits"
  printf 'PHASED    %-55s CRDs=%s controllers=%s certificates=%s\n' \
    "${operator}" "${crd_count}" "${controller_count}" "${certificate_count}"
done

note "GitOps static validation passed; no cluster API or release-artifact fetch was performed"
