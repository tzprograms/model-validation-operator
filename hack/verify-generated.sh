#!/usr/bin/env bash
# Copyright 2026 The Sigstore Authors.
# Licensed under the Apache License, Version 2.0 (the "License");
set -euo pipefail

project_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$project_root"

make manifests generate
paths=(config/crd/bases config/rbac config/webhook api)
git diff --exit-code -- "${paths[@]}"
untracked=$(git ls-files --others --exclude-standard -- "${paths[@]}")
if [[ -n "$untracked" ]]; then
    printf 'Generated artifacts are missing from Git:\n%s\n' "$untracked" >&2
    exit 1
fi
