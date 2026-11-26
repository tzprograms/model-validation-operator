#!/usr/bin/env bash
# Exercise the real checker against temporary Git history and both CRDs.
set -euo pipefail
project_root=$(cd "$(dirname "$0")/.." && pwd)
checker=${CRDIFY:-$(command -v crdify)}
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/hack" "$scratch/config/crd"
cp "$project_root/hack/check-crd-compatibility.sh" "$scratch/hack/"
cp "$project_root/crdify.yaml" "$scratch/"
cp -R "$project_root/config/crd/bases" "$scratch/config/crd/"
cd "$scratch"
git init -q
git add .
git -c user.name='CRD check fixture' -c user.email='fixture@example.invalid' \
    -c commit.gpgsign=false commit -qm 'Initial schema fixture'
export BASE_SHA
BASE_SHA=$(git rev-parse HEAD)
export CRDIFY="$checker"

bash hack/check-crd-compatibility.sh > equal.log
[[ $(grep -c '^Checking ' equal.log) -eq 2 ]]
echo 'PASS: compares both unchanged CRDs'

for crd in config/crd/bases/*.yaml; do
    python3 - "$crd" <<'PY'
from pathlib import Path
import sys
path = Path(sys.argv[1])
source = path.read_text()
field = 'path' if 'modelvalidations' in path.name else 'endpoint'
start = source.index('\n                  ' + field + ':\n')
position = source.index('type: string', start)
path.write_text(source[:position] + source[position:].replace('type: string', 'type: integer', 1))
PY
    if bash hack/check-crd-compatibility.sh > breaking.log 2>&1; then
        echo "FAIL: accepted incompatible schema in $crd" >&2
        exit 1
    fi
    git restore "$crd"
    echo "PASS: rejects incompatible type in $crd"
done

crd=config/crd/bases/ml.sigstore.dev_telemetryconfigs.yaml
rm "$crd"
if bash hack/check-crd-compatibility.sh > removed.log 2>&1; then
    echo 'FAIL: accepted removal of an existing CRD' >&2
    exit 1
fi
grep -q 'Removing an existing CRD is incompatible' removed.log
git restore "$crd"
echo 'PASS: rejects removal of an existing CRD'

cp "$crd" config/crd/bases/new-fixture.yaml
git add config/crd/bases/new-fixture.yaml
bash hack/check-crd-compatibility.sh > added.log
grep -q 'New CRD has no previous schema' added.log
echo 'PASS: explicitly handles a new CRD'

if BASE_SHA=not-a-commit bash hack/check-crd-compatibility.sh > invalid.log 2>&1; then
    echo 'FAIL: accepted invalid base revision' >&2
    exit 1
fi
echo 'PASS: rejects invalid base revision'
BASE_SHA=0000000000000000000000000000000000000000 \
    bash hack/check-crd-compatibility.sh | grep -q 'No previous revision'
echo 'PASS: reports the missing-history case explicitly'
