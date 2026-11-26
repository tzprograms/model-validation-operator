#!/usr/bin/env bash
# Copyright 2026 The Sigstore Authors.
# Licensed under the Apache License, Version 2.0 (the "License");
set -euo pipefail

project_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$project_root"
base=${BASE_SHA:-}
if [[ -z "$base" || "$base" =~ ^0+$ ]]; then
    echo "No previous revision is available; skipping CRD comparison"
    exit 0
fi
[[ "$base" =~ ^[a-fA-F0-9]{40}$ ]] || { echo "Invalid base commit" >&2; exit 1; }
git cat-file -e "$base^{commit}"
checker=${CRDIFY:-crdify}
snapshot_dir=$(mktemp -d)
trap 'rm -rf "$snapshot_dir"' EXIT

git ls-tree -r --name-only "$base" -- config/crd/bases > "$snapshot_dir/base-files"
git ls-files -- config/crd/bases > "$snapshot_dir/current-files"
while IFS= read -r path; do
    [[ "$path" == *.yaml || "$path" == *.yml ]] || continue
    if [[ ! -f "$path" ]]; then
        printf 'Removing an existing CRD is incompatible: %s\n' "$path" >&2
        exit 1
    fi
    git show "$base:$path" > "$snapshot_dir/base-crd.yaml"
    printf 'Checking %s\n' "$path"
    "$checker" --config crdify.yaml "file://$snapshot_dir/base-crd.yaml" "file://$path"
done < "$snapshot_dir/base-files"

while IFS= read -r path; do
    [[ "$path" == *.yaml || "$path" == *.yml ]] || continue
    if ! grep -Fxq "$path" "$snapshot_dir/base-files"; then
        printf 'New CRD has no previous schema to compare: %s\n' "$path"
    fi
done < "$snapshot_dir/current-files"
