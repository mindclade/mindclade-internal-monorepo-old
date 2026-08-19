# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Clean and hygiene targets for a Bazel/Go/Rust/Python/TypeScript monorepo.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -u -o pipefail -c

DRY_RUN ?= 0
CLEAN_CONFIRM ?= 0

BAZEL := $(shell \
	if command -v bazelisk >/dev/null 2>&1; then \
		echo bazelisk; \
	elif command -v bazel >/dev/null 2>&1; then \
		echo bazel; \
	fi)

.PHONY: help check-root check-bazel clean clean-bazel clean-python clean-rust clean-node clean-local clean-git clean-all clean-dry

help:
	@echo "Mindclade repository cleanup"
	@echo "Usage:"
	@echo "  make clean"
	@echo "  make clean-bazel      # Bazel --expunge"
	@echo "  make clean-python     # Python caches and .py[cod], coverage artifacts"
	@echo "  make clean-rust       # Rust/Cargo target directories"
	@echo "  make clean-node       # Node/TypeScript artifacts"
	@echo "  make clean-local      # Bazel, Nix, Terraform, env local outputs"
	@echo "  make clean-git        # Ignored+untracked (Bazel/ci style), safe preview by default"
	@echo "  make clean-all        # clean + clean-git"
	@echo "  make clean-dry        # Dry-run preview for all cleanup targets"
	@echo
	@echo "Options:"
	@echo "  DRY_RUN=1            # preview only, no destructive operations"
	@echo "  CLEAN_CONFIRM=1       # required to run 'make clean-git' and 'make clean-all'"

check-root:
	@if [ ! -f BUILD.bazel ] || [ ! -f MODULE.bazel ] || [ ! -d .git ]; then \
		echo "ERROR: run from repository root (BUILD.bazel, MODULE.bazel, and .git required)." >&2; \
		exit 1; \
	fi

check-bazel:
	@if [ -z "$(BAZEL)" ]; then \
		echo "ERROR: bazelisk or bazel must be on PATH." >&2; \
		exit 1; \
	fi
	@echo "Using Bazel binary: $(BAZEL)"

clean: check-root clean-bazel clean-python clean-rust clean-node clean-local

clean-bazel: check-root check-bazel
	@if [ "$(DRY_RUN)" = "1" ]; then \
		echo "DRY-RUN: would run: $(BAZEL) clean --expunge"; \
	else \
		$(BAZEL) clean --expunge; \
	fi

clean-python: check-root
	@{
		mapfile -d '' -t targets < <(find . \
			-path '*/.git' -prune -o \
			-type d \( -name '__pycache__' -o -name '.pytest_cache' -o -name '.mypy_cache' -o -name '.ruff_cache' \) -print0; \
			find . \
			-path '*/.git' -prune -o \
			-type f \( -name '*.pyc' -o -name '*.pyo' -o -name '*.pyd' \) -print0; \
			find . \
			-path '*/.git' -prune -o \
			-type f \( -name '.coverage' -o -name 'coverage.xml' \) -print0; \
			find . \
			-path '*/.git' -prune -o -type d -name 'htmlcov' -print0);
		if [ "${#targets[@]}" -eq 0 ]; then \
			echo "No Python intermediates found."; \
		elif [ "$(DRY_RUN)" = "1" ]; then \
			printf 'DRY-RUN: would remove: %s\n' "${targets[@]}"; \
		else \
			printf '%s\0' "${targets[@]}" | xargs -0 rm -rf --; \
		fi; \
	}

clean-rust: check-root
	@{
		mapfile -d '' -t targets < <(find . \
			-path '*/.git' -prune -o \
			-type d -name 'target' -print0);
		if [ "${#targets[@]}" -eq 0 ]; then \
			echo "No Rust/Cargo target directories found."; \
		elif [ "$(DRY_RUN)" = "1" ]; then \
			printf 'DRY-RUN: would remove: %s\n' "${targets[@]}"; \
		else \
			printf '%s\0' "${targets[@]}" | xargs -0 rm -rf --; \
		fi; \
	}

clean-node: check-root
	@{
		mapfile -d '' -t targets < <(find . \
			-path '*/.git' -prune -o \
			-type d \( -name 'node_modules' -o -name '.next' -o -name '.pnpm-store' \) -print0);
		for d in apps/*/dist libs/ts/*/dist sdk/typescript/dist; do \
			[ -d "$$d" ] && targets+=( "$$d" ); \
		done
		{
			printf '%s\0' "${targets[@]}" | tr '\0' '\n' | sort -u;
		} | mapfile -t targets;
		if [ "${#targets[@]}" -eq 0 ]; then \
			echo "No Node/TS intermediates found."; \
		elif [ "$(DRY_RUN)" = "1" ]; then \
			printf 'DRY-RUN: would remove: %s\n' "${targets[@]}"; \
		else \
			printf '%s\0' "${targets[@]}" | xargs -0 rm -rf --; \
		fi; \
	}

clean-local: check-root
	@{
		mapfile -d '' -t targets < <(for p in \
			"bazel-bin" "bazel-out" "bazel-testlogs" "bazel-mindclade" \
			".bazel-cache" ".direnv" "result" "result-*" ".terraform" ".terragrunt-cache" \
			".pnpm-store" ".next" "bazel-*"; do \
				for f in $$p; do \
					if [ -e "$$f" ]; then \
						printf '%s\0' "$$f"; \
					fi; \
				done; \
			done; \
		)
		if [ "${#targets[@]}" -eq 0 ]; then \
			echo "No local intermediates found."; \
		elif [ "$(DRY_RUN)" = "1" ]; then \
			printf 'DRY-RUN: would remove: %s\n' "${targets[@]}"; \
		else \
			printf '%s\0' "${targets[@]}" | xargs -0 rm -rf --; \
		fi; \
	}

clean-git: check-root
	@echo "Preview: ignored + untracked (dry-run style):"
	@git clean -ndX
	@if [ "$(DRY_RUN)" = "1" ]; then \
		echo "DRY-RUN=1: skipping destructive git clean."; \
	else \
		if [ "$(CLEAN_CONFIRM)" != "1" ]; then \
			echo "Set CLEAN_CONFIRM=1 to run destructive git clean." >&2; \
			exit 1; \
		fi; \
		git clean -fdX; \
	fi

clean-all: clean clean-git

clean-dry:
	@$(MAKE) clean DRY_RUN=1
