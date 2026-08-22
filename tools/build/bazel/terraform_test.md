# Offline Terraform Tests

Use `terraform_test` for Terraform module tests that must run hermetically under
Bazel:

~~~starlark
load("//tools/build/bazel:terraform_test.bzl", "terraform_test")

terraform_test(
    name = "terraform_test",
    module_files = [":scaffold_files"],
    module_marker = "variables.tf",
)
~~~

The rule uses the checksum-pinned Google provider repository and the pinned
infrastructure tool bundle. Its CLI configuration contains only a filesystem
mirror, with no direct registry fallback. The shared runner performs normal
`terraform init -backend=false -lockfile=readonly` followed by `terraform test`,
so Terraform owns module-manifest generation rather than a module-specific test
script.

The `module_marker` must name a file at the module root. Include all module source,
lock, fixture, and test files in `module_files`.
