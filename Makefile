# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#
# Clean and hygiene targets for a Bazel/Go/Rust/Python/TypeScript monorepo.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -u -o pipefail -c
.DEFAULT_GOAL := help

DRY_RUN ?= 0
CLEAN_CONFIRM ?= 0

BAZEL := $(shell \
	if command -v bazelisk >/dev/null 2>&1; then \
		echo bazelisk; \
	elif command -v bazel >/dev/null 2>&1; then \
		echo bazel; \
	fi)

.PHONY: help \
	check-root \
	check-bazel \
	selfcheck \
	clean \
	clean-soft \
	clean-deep \
	clean-bazel \
	clean-python \
	clean-rust \
	clean-go \
	clean-node \
	clean-local \
	clean-git \
	clean-all \
	clean-dry

help:
	@echo "Mindclade repository cleanup"
	@echo "Usage:"
	@echo "  make clean           # Production-clean baseline: python/rust/node/local/go + Bazel --expunge"
	@echo "  make clean-soft       # Fast local cleanup (python/rust/node/local/go)"
	@echo "  make clean-deep       # Deep reset (adds bazel --expunge + git ignored/untracked cleanup)"
	@echo "  make clean-bazel      # Bazel --expunge only"
	@echo "  make clean-python     # Python caches and .py[cod], coverage artifacts"
	@echo "  make clean-rust       # Rust/Cargo target directories"
	@echo "  make clean-go         # Go cache/testcache/modcache"
	@echo "  make clean-node       # Node/TypeScript artifacts"
	@echo "  make clean-local      # Bazel, Nix, Terraform, env local outputs"
	@echo "  make clean-git        # Ignored+untracked (Bazel/ci style), safe preview by default"
	@echo "  make clean-all        # full clean+git cleanup"
	@echo "  make clean-dry        # Dry-run preview for all cleanup targets"
	@echo "  make selfcheck        # Validate tooling required by clean targets"
	@echo
	@echo "Options:"
	@echo "  DRY_RUN=1            # preview only, no destructive operations"
	@echo "  CLEAN_CONFIRM=1       # required to run 'make clean-git' and 'make clean-all'"

check-root:
	@if [ ! -f BUILD.bazel ] || [ ! -f MODULE.bazel ] || [ ! -d .git ]; then \
		echo "ERROR: run from repository root (BUILD.bazel, MODULE.bazel, and .git required)." >&2; \
		exit 1; \
	fi

selfcheck: check-root
	@for cmd in find xargs git; do \
		if ! command -v "$$cmd" >/dev/null 2>&1; then \
			echo "ERROR: required command missing: $$cmd" >&2; \
			exit 1; \
		fi; \
	done

check-bazel:
	@if [ -z "$(BAZEL)" ]; then \
		echo "ERROR: bazelisk or bazel must be on PATH." >&2; \
		exit 1; \
	fi
	@echo "Using Bazel binary: $(BAZEL)"

clean: selfcheck clean-soft clean-bazel

clean-bazel: check-root check-bazel
	@if [ "$(DRY_RUN)" = "1" ]; then \
		echo "DRY-RUN: would run: $(BAZEL) clean --expunge"; \
	else \
		$(BAZEL) clean --expunge; \
	fi

clean-soft: selfcheck clean-python clean-rust clean-go clean-node clean-local

clean-python: check-root
	@tmp=$$(mktemp); \
	trap 'rm -f "$$tmp"' EXIT; \
	( \
		find . \
			-path '*/.git' -prune -o \
			-type d \( -name '__pycache__' -o -name '.pytest_cache' -o -name '.mypy_cache' -o -name '.ruff_cache' \) -print0; \
		find . \
			-path '*/.git' -prune -o \
			-type f \( -name '*.pyc' -o -name '*.pyo' -o -name '*.pyd' \) -print0; \
		find . \
			-path '*/.git' -prune -o \
			-type f \( -name '.coverage' -o -name 'coverage.xml' \) -print0; \
		find . \
			-path '*/.git' -prune -o \
			-type d -name 'htmlcov' -print0; \
	) > "$$tmp"; \
	if [ ! -s "$$tmp" ]; then \
		echo "No Python intermediates found."; \
	elif [ "$(DRY_RUN)" = "1" ]; then \
		while IFS= read -r -d '' p; do \
			echo "[dry-run] would remove: $$p"; \
		done < "$$tmp"; \
	else \
		xargs -0 rm -rf -- < "$$tmp"; \
	fi

clean-rust: check-root
	@tmp=$$(mktemp); \
	trap 'rm -f "$$tmp"' EXIT; \
	find . \
		-path '*/.git' -prune -o \
		-type d -name 'target' -print0 > "$$tmp"; \
	if [ ! -s "$$tmp" ]; then \
		echo "No Rust/Cargo target directories found."; \
	elif [ "$(DRY_RUN)" = "1" ]; then \
		while IFS= read -r -d '' p; do \
			echo "[dry-run] would remove: $$p"; \
		done < "$$tmp"; \
	else \
		xargs -0 rm -rf -- < "$$tmp"; \
	fi

clean-go: check-root
	@if ! command -v go >/dev/null 2>&1; then \
		echo "SKIP: go binary not found"; \
	elif [ "$(DRY_RUN)" = "1" ]; then \
		echo "[dry-run] would run: go clean -cache -testcache -modcache"; \
	else \
		GOWORK=off go clean -cache -testcache -modcache; \
	fi

clean-node: check-root
	@tmp=$$(mktemp); \
	trap 'rm -f "$$tmp"' EXIT; \
	( \
		find . \
			-path '*/.git' -prune -o \
			-type d \( -name 'node_modules' -o -name '.next' -o -name '.pnpm-store' \) -print0; \
		for d in apps/*/dist libs/ts/*/dist sdk/typescript/dist; do \
			[ -d "$$d" ] && printf '%s\0' "$$d"; \
		done \
	) > "$$tmp"; \
	if [ ! -s "$$tmp" ]; then \
		echo "No Node/TS intermediates found."; \
	elif [ "$(DRY_RUN)" = "1" ]; then \
		while IFS= read -r -d '' p; do \
			echo "[dry-run] would remove: $$p"; \
		done < "$$tmp"; \
	else \
		xargs -0 rm -rf -- < "$$tmp"; \
	fi

clean-local: check-root
	@tmp=$$(mktemp); \
	trap 'rm -f "$$tmp"' EXIT; \
	( \
		for p in \
			bazel-* \
			.bazel-cache .direnv result result-* .terraform .terragrunt-cache \
			.pnpm-store .next; do \
			for f in $$p; do \
				[ -e "$$f" ] && printf '%s\0' "$$f"; \
			done; \
		done \
	) > "$$tmp"; \
	if [ ! -s "$$tmp" ]; then \
		echo "No local intermediates found."; \
	elif [ "$(DRY_RUN)" = "1" ]; then \
		while IFS= read -r -d '' p; do \
			echo "[dry-run] would remove: $$p"; \
		done < "$$tmp"; \
	else \
		xargs -0 rm -rf -- < "$$tmp"; \
	fi

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

clean-deep: clean clean-git

clean-all: clean-deep

clean-dry:
	@$(MAKE) clean DRY_RUN=1
