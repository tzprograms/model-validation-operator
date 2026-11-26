#!/usr/bin/env bash
set -euo pipefail

# envtest's external etcd uses SHA-1 for non-cryptographic cluster IDs. Keep
# that fixture in normal FIPS mode; the Go test process remains in strict mode.
# TEST_ASSET_ETCD selects this wrapper without changing the installed asset.
: "${KUBEBUILDER_ASSETS:?set KUBEBUILDER_ASSETS to the envtest binary directory}"
exec env GODEBUG="${GODEBUG:+${GODEBUG},}fips140=on" "${KUBEBUILDER_ASSETS}/etcd" "$@"
