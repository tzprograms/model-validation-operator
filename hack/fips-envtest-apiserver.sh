#!/usr/bin/env bash
set -euo pipefail

# The external envtest apiserver uses an older Go TLS implementation that
# attempts X25519 in strict mode. Keep this fixture in normal FIPS mode;
# the operator's Go test process remains in strict mode.
: "${KUBEBUILDER_ASSETS:?set KUBEBUILDER_ASSETS to the envtest binary directory}"
exec env GODEBUG="${GODEBUG:+${GODEBUG},}fips140=on" "${KUBEBUILDER_ASSETS}/kube-apiserver" "$@"
