# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
"""Pinned schema inputs and the transitional Nix-to-Bazel infra validation bridge.

The schema repository is a normal fixed-output Bazel repository: every URL names an
immutable upstream revision and every file has a SHA-256.  The CLI repository is narrower
and intentionally transitional.  Nix remains the repository's tool-version authority, so
CI enters `.#ci`; this rule captures those executables as declared Bazel inputs instead of
letting tests inherit the client PATH or opt out of sandboxing.  The validators verify the
captured versions against //tools/build/nix:toolchain-manifest.json before using them.
"""

_TOOLS = [
    "conftest",
    "helm",
    "kubeconform",
    "kustomize",
    "promtool",
    "python3",
    "terraform",
    "yamllint",
    "yq",
]

_TERRAFORM_GOOGLE_PROVIDER_VERSION = "7.45.0"
_TERRAFORM_GOOGLE_PROVIDER_SHA256 = {
    "darwin_amd64": "58dadd7b96b7b706e2995a04f356862be7cfd6b55f6257464c8b10986abf8efa",
    "darwin_arm64": "172c9e4902e9a01b1111bbbb9063a47804ec5f4207b2221df9c398239ff3b350",
    "linux_amd64": "fb1b9d1ea7bc79b7409f02aa7c19ba39afa22dbead69e83ae7eb2691ac5c2426",
    "linux_arm64": "7230a5e49abed243317b9de0ee7fd365b4a2775b532a5d66006b39c4703978ac",
}

_KUBERNETES_SCHEMA_REVISION = "c8f4e61c63bc529749125ac566bccc6986e08d45"
_KUBERNETES_SCHEMA_BASE = (
    "https://raw.githubusercontent.com/yannh/kubernetes-json-schema/" +
    _KUBERNETES_SCHEMA_REVISION +
    "/v1.36.2-standalone-strict/"
)

# Only schemas exercised by the rendered inventory are fetched.  Inventory drift is
# fail-closed because kubeconform is not passed --ignore-missing-schemas.
_KUBERNETES_SCHEMAS = {
    "apiservice-apiregistration-v1.json": "da57df9a196682ca1502feeeda7eb9f7861433d47702a0589cf93f80fcd92847",
    "clusterrole-rbac-v1.json": "0777f91c4fa08bd3989b6db8c59dad0429a63dec8f59e883ff55c947a14d0d5f",
    "clusterrolebinding-rbac-v1.json": "e44498f0ead69333bf1d066dec35470f8bf54898ec2647c9e60f8396aa26f04a",
    "configmap-v1.json": "e0eaddebd677c08aa092b2da2264d86ac4fc34eed112b9fac2945b3f00c1e9b1",
    "deployment-apps-v1.json": "3725782fb01e3f27d8be2da565e2d653d7b78bf6debe5440804cea993c87b8f9",
    "horizontalpodautoscaler-autoscaling-v2.json": "dae451f66e51f43b9c54255f4ed6684c22afc2e429b587d476bd057f1170e5e6",
    "job-batch-v1.json": "f49fee30367890a492146d63c3c433a7853e9088ebfe9e25cc792ec4b1400f18",
    "limitrange-v1.json": "de9bfabf9ea0bff36b71e5e686d3ecce5246b098061f39a70549f82f8f03acd7",
    "mutatingwebhookconfiguration-admissionregistration-v1.json": "f6d7abff06dc187551017d0bb188940f41ae75f78612211df95a447c686b8711",
    "namespace-v1.json": "324fae677b98d1a6d54340db0c334d053e8ffbafceb3f73326e41de2610d5843",
    "networkpolicy-networking-v1.json": "f6324cc464f62228b0418f438d167208e4f86c7e3677ba30f608e79a8b26ba79",
    "poddisruptionbudget-policy-v1.json": "92b2b2cf7c193b9d10943e7785ebe6eefce439d9e1e34e5fd9e820155a657137",
    "priorityclass-scheduling-v1.json": "614e8878ac82099cccc0c55e90f487c09ced6057954cb1c5189f6cccc5799451",
    "resourcequota-v1.json": "9546ed313e622ea84a0ab1ec7ef7e683f05acfbfe97beb746f0b5be6407691df",
    "role-rbac-v1.json": "dfe8fb03b1642f985b57b12edd1727b6867c13ed4b6761ef81bdd51b1d236711",
    "rolebinding-rbac-v1.json": "b12ea4163fc37df2e02192b4291b1b7c28836e3e7665d989dd538bfcffa10076",
    "service-v1.json": "8bf019854daed511e7c174896a898173fa65d88ec5937c687a37303d4cc9351b",
    "serviceaccount-v1.json": "8193d6c3561475c6d3d5c44e1faedb1df53905373d904bc17015694326d659cf",
    "validatingadmissionpolicy-admissionregistration-v1.json": "6f694e5637b097f781139af4fc466afa63b992d3dc55d28808028c99746be35e",
    "validatingadmissionpolicybinding-admissionregistration-v1.json": "829b6a2fabcad663ba303c13e493a9b8c18a5737b0ca91c45dc0cf49a24c3927",
    "validatingwebhookconfiguration-admissionregistration-v1.json": "e3119eb8530edcd3b114b653d2291f9c56bd62ba62349db23d6e4c8fc1eb8bda",
}

_PINNED_CRDS = {
    "cert-manager-v1.19.1.yaml": (
        "https://github.com/cert-manager/cert-manager/releases/download/v1.19.1/cert-manager.yaml",
        "876a41a57e36b85619f4124b24b3deb80912b5ffed515f90e2f160b6e6338e81",
    ),
    "gateway.networking.k8s.io_gateways.yaml": (
        "https://raw.githubusercontent.com/kubernetes-sigs/gateway-api/477d172e6ac5eccb82b65781ddb8f924afec4170/config/crd/standard/gateway.networking.k8s.io_gateways.yaml",
        "a02ea425fc901f197b668c9ddd56375e1f6896994914c6e6b9b4fdb85cf3ba6e",
    ),
    "gateway.networking.k8s.io_httproutes.yaml": (
        "https://raw.githubusercontent.com/kubernetes-sigs/gateway-api/477d172e6ac5eccb82b65781ddb8f924afec4170/config/crd/standard/gateway.networking.k8s.io_httproutes.yaml",
        "98c6777c22309d319292e9c288ee632006c9ffdd4272383d6f9dffa3fbccaf14",
    ),
    "monitoring.googleapis.com_podmonitorings.yaml": (
        "https://raw.githubusercontent.com/GoogleCloudPlatform/prometheus-engine/82d36e33af2fbedd0a58af10bb6f000aaddef832/charts/operator/crds/monitoring.googleapis.com_podmonitorings.yaml",
        "e14f6b033dae70890e27d69943dc1b6c0874ad0db73425483e2e4a65597d2e19",
    ),
    "monitoring.googleapis.com_rules.yaml": (
        "https://raw.githubusercontent.com/GoogleCloudPlatform/prometheus-engine/82d36e33af2fbedd0a58af10bb6f000aaddef832/charts/operator/crds/monitoring.googleapis.com_rules.yaml",
        "e2759cd100c2d29cf90095659d2ccb6f2f710d5666d2867371369f322fe17797",
    ),
    "networking.gke.io_gcpbackendpolicies.yaml": (
        "https://raw.githubusercontent.com/GoogleCloudPlatform/gke-gateway-api/a663dec06e3fa9e07fd252480b1f55043dd1863a/config/crd/networking.gke.io_gcpbackendpolicies.yaml",
        "4386a156a7f616f857dea99eba7c740f30062d972a9dc9a1a4659237f92c2262",
    ),
}

def _validation_tools_repository_impl(repository_ctx):
    missing = []
    for tool in _TOOLS:
        path = repository_ctx.which(tool)
        if path == None:
            missing.append(tool)
        else:
            repository_ctx.symlink(path, "bin/" + tool)

    if missing:
        fail(
            "infra validation tools are missing: %s; run Bazel through " % ", ".join(missing) +
            "`nix develop .#ci --command ...` so the pinned closure can be captured",
        )

    repository_ctx.file("bin/.tool-root", "Nix-owned validation tool bridge\n")
    repository_ctx.file(
        "BUILD.bazel",
        """package(default_visibility = [\"//visibility:public\"])

exports_files([\"bin/.tool-root\"])

filegroup(
    name = \"tools\",
    srcs = glob([\"bin/*\"]),
)
""",
    )

_validation_tools_repository = repository_rule(
    implementation = _validation_tools_repository_impl,
    environ = ["PATH"],
    local = True,
)

def _terraform_google_provider_repository_impl(repository_ctx):
    os_name = repository_ctx.os.name.lower()
    if os_name.startswith("mac"):
        operating_system = "darwin"
    elif os_name.startswith("linux"):
        operating_system = "linux"
    else:
        fail("unsupported Terraform provider host operating system: %s" % repository_ctx.os.name)

    architecture_name = repository_ctx.os.arch.lower()
    if architecture_name in ["aarch64", "arm64"]:
        architecture = "arm64"
    elif architecture_name in ["amd64", "x86_64"]:
        architecture = "amd64"
    else:
        fail("unsupported Terraform provider host architecture: %s" % repository_ctx.os.arch)

    platform = operating_system + "_" + architecture
    archive_name = "terraform-provider-google_%s_%s.zip" % (
        _TERRAFORM_GOOGLE_PROVIDER_VERSION,
        platform,
    )
    provider_dir = "providers/registry.terraform.io/hashicorp/google/%s/%s" % (
        _TERRAFORM_GOOGLE_PROVIDER_VERSION,
        platform,
    )
    repository_ctx.download_and_extract(
        output = provider_dir,
        sha256 = _TERRAFORM_GOOGLE_PROVIDER_SHA256[platform],
        url = "https://releases.hashicorp.com/terraform-provider-google/%s/%s" % (
            _TERRAFORM_GOOGLE_PROVIDER_VERSION,
            archive_name,
        ),
    )

    provider_root = "providers/.provider-root"
    repository_ctx.file(provider_root, platform + "\n")
    repository_ctx.file(
        "BUILD.bazel",
        """package(default_visibility = [\"//visibility:public\"])

filegroup(
    name = \"provider_root\",
    srcs = [\"%s\"],
)

filegroup(
    name = \"provider\",
    srcs = glob([\"providers/**\"]),
)
""" % provider_root,
    )

_terraform_google_provider_repository = repository_rule(
    implementation = _terraform_google_provider_repository_impl,
    configure = True,
)

def _kubernetes_schemas_repository_impl(repository_ctx):
    for name, sha256 in _KUBERNETES_SCHEMAS.items():
        repository_ctx.download(
            output = "schemas/" + name,
            sha256 = sha256,
            url = _KUBERNETES_SCHEMA_BASE + name,
        )

    for name, source in _PINNED_CRDS.items():
        repository_ctx.download(
            output = "crds/" + name,
            sha256 = source[1],
            url = source[0],
        )

    repository_ctx.file("schemas/.schema-root", _KUBERNETES_SCHEMA_REVISION + "\n")
    repository_ctx.file("crds/.crd-root", "fixed-output custom CRDs\n")
    repository_ctx.file(
        "BUILD.bazel",
        """package(default_visibility = [\"//visibility:public\"])

exports_files([
    \"crds/.crd-root\",
    \"schemas/.schema-root\",
])

filegroup(
    name = \"core_schemas\",
    srcs = glob([\"schemas/*.json\"]),
)

filegroup(
    name = \"custom_crds\",
    srcs = glob([\"crds/*.yaml\"]),
)
""",
    )

_kubernetes_schemas_repository = repository_rule(
    implementation = _kubernetes_schemas_repository_impl,
)

def _infra_validation_impl(_module_ctx):
    _validation_tools_repository(name = "mindclade_infra_validation_tools")
    _terraform_google_provider_repository(name = "mindclade_terraform_google_provider")
    _kubernetes_schemas_repository(name = "mindclade_kubernetes_schemas")

# The extension always declares the same three repositories. The validation-tool repository is
# local and PATH-sensitive, so Bazel refreshes its symlinks independently of the module lock;
# marking the extension OS-dependent would instead require committing a lock entry generated
# on every CI host OS even though the generated repository specifications are identical. The
# configured provider repository selects one of four fixed-output HashiCorp artifacts for the
# execution host without allowing Terraform tests to contact the provider registry.
infra_validation = module_extension(
    implementation = _infra_validation_impl,
)
