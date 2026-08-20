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

for tool_name in cp diff find grep helm kubeconform kustomize mkdir mktemp rg sed sort yamllint yq; do
  require_tool "${tool_name}"
done

yq_version="$(yq --version 2>&1)"
[[ "${yq_version}" == *"mikefarah/yq"* ]] ||
  fail "mikefarah/yq v4 is required; found ${yq_version}"

validation_tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/mindclade-gitops-validation.XXXXXX")"
cleanup() {
  rm -rf -- "${validation_tmp_dir}"
}
trap cleanup EXIT

# Bazel materializes data as symlinks. Kustomize intentionally refuses a symlinked
# kustomization.yaml under RootOnly, so stage only the declared GitOps/Kubernetes inputs as real
# files inside this validator-owned temporary directory. Source-tree execution needs no copy.
if [[ -L "${gitops_root}/environments/development/kustomization.yaml" ]]; then
  staged_repository="${validation_tmp_dir}/repository"
  mkdir -p "${staged_repository}/infra"
  cp -R -L "${gitops_root}" "${staged_repository}/infra/gitops"
  cp -R -L "${repository_root}/infra/kubernetes" "${staged_repository}/infra/kubernetes"
  repository_root="${staged_repository}"
  gitops_root="${staged_repository}/infra/gitops"
fi

note "parsing owned YAML and rejecting stale or credential-bearing contracts"
while IFS= read -r -d '' yaml_file; do
  yq eval-all '.' "${yaml_file}" >/dev/null || fail "invalid YAML: ${yaml_file}"
done < <(find "${gitops_root}" -type f \( -name '*.yaml' -o -name '*.yml' \) -print0)

yamllint --strict --config-file "${validation_support_dir}/yamllint.yaml" "${gitops_root}"

if rg -n 'gitops/scripts/render\.sh|\./scripts/render\.sh' "${gitops_root}" >/dev/null; then
  fail "stale reference to the nonexistent render.sh workflow"
fi

if rg -n '^kind:[[:space:]]+Secret$' "${gitops_root}" --glob '*.yaml' --glob '*.yml' >/dev/null; then
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
[[ "${project_count}" == "4" ]] || fail "expected four scoped AppProjects, found ${project_count}"

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
[[ "$(yq eval -r '.data."argocd.installation-state"' "${lock_file}")" == blocked-* ]] ||
  fail "Argo CD bootstrap must stay blocked until the live repository supplies its chart lock"

for chart_lock_key in jobset kueue; do
  chart_lock_path="$(yq eval -r ".data.\"${chart_lock_key}.chart-lock\"" "${lock_file}")"
  [[ -f "${repository_root}/${chart_lock_path}" ]] ||
    fail "declared chart lock does not exist: ${chart_lock_path}"
done

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

  kubeconform -exit-on-error -kubernetes-version 1.36.0 -strict -summary \
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
  validate_paused_applications "${bootstrap_render}" 5 "${environment} child applications"

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

  printf 'VALIDATED %-51s foundation and five paused applications\n' "${environment}"
done

note "linting locked operator charts with cert-manager integration"
for operator in kueue jobset; do
  chart_dir="${repository_root}/infra/kubernetes/platform/${operator}/chart"
  rendered_chart="${validation_tmp_dir}/${operator}-chart.yaml"
  [[ -f "${chart_dir}/Chart.lock" ]] || fail "${operator}: Chart.lock is missing"
  [[ -d "${chart_dir}/charts" ]] || fail "${operator}: vendored chart dependency is missing"
  helm lint --strict "${chart_dir}" >/dev/null
  if [[ "${operator}" == "kueue" ]]; then
    helm template "mindclade-${operator}" "${chart_dir}" --namespace "${operator}-system" \
      --include-crds --set kueue.enableCertManager=true >"${rendered_chart}"
  else
    helm template "mindclade-${operator}" "${chart_dir}" --namespace "${operator}-system" \
      --include-crds --set jobset.certManager.enable=true >"${rendered_chart}"
  fi

  authored_secret_count="$({
    yq eval-all '[select(.kind == "Secret")] | length' "${rendered_chart}"
  })"
  [[ "${authored_secret_count}" == "0" ]] ||
    fail "${operator}: cert-manager integration must not render a private-key Secret"

  crd_count="$(yq eval-all '[select(.kind == "CustomResourceDefinition")] | length' \
    "${rendered_chart}")"
  controller_count="$(yq eval-all '[select(.kind == "Deployment")] | length' \
    "${rendered_chart}")"
  certificate_count="$(yq eval-all '[select(.kind == "Certificate")] | length' \
    "${rendered_chart}")"
  [[ "${crd_count}" -gt 0 && "${controller_count}" -gt 0 && "${certificate_count}" -gt 0 ]] ||
    fail "${operator}: chart must render CRDs, a controller, and cert-manager Certificates"

  unpinned_image_count="$({
    yq eval-all '
      [select(.kind == "Deployment") | .spec.template.spec.containers[].image |
        select(test("@sha256:[0-9a-f]{64}$") == false)
      ] | length
    ' "${rendered_chart}"
  })"
  [[ "${unpinned_image_count}" == "0" ]] ||
    fail "${operator}: controller chart rendered a mutable container image"
  printf 'HELM      %-55s CRDs=%s controllers=%s certificates=%s\n' \
    "${operator}" "${crd_count}" "${controller_count}" "${certificate_count}"
done

note "GitOps static validation passed; no cluster API or release-artifact fetch was performed"
