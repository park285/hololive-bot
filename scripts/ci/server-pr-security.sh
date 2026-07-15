#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.5+auto}"
export GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.4.0}"
export GOSEC_VERSION="${GOSEC_VERSION:-v2.27.1}"
export NILAWAY_VERSION="${NILAWAY_VERSION:-v0.0.0-20260617211854-01ab7e30fbe0}"
export GOBIN="${RUNNER_TEMP:-/tmp}/hololive-go-tools"
mkdir -p "${GOBIN}"
export PATH="${GOBIN}:${PATH}"

echo "[server-pr-security] install pinned analyzers"
go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
go install "github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION}"
go install "go.uber.org/nilaway/cmd/nilaway@${NILAWAY_VERSION}"

source scripts/ci/go-workspace-modules.sh
mapfile -t owned_packages < <(
  {
    printf './...\n'
    go_workspace_package_patterns | sed 's#^\./\.\./#../#'
  } | awk 'NF' | grep -vE '^\.\./(shared-go|iris-client-go)/\.\.\.$'
)

if (( ${#owned_packages[@]} == 0 )); then
  echo "no repository-owned Go package patterns resolved" >&2
  exit 1
fi

echo "[server-pr-security] gosec"
gosec -quiet -fmt text "${owned_packages[@]}"

echo "[server-pr-security] NilAway"
export GOMEMLIMIT="${GOMEMLIMIT:-5GiB}"
export GOMAXPROCS="${GOMAXPROCS:-2}"
for package in "${owned_packages[@]}"; do
  echo "nilaway ${package}"
  nilaway -pretty-print "${package}"
done

echo "[server-pr-security] govulncheck"
mapfile -t owned_modules < <(
  go work edit -json | python3 -c '
import json, pathlib, sys
root = pathlib.Path.cwd().resolve()
for entry in json.load(sys.stdin)["Use"]:
    raw = entry["DiskPath"]
    path = (root / raw).resolve()
    if path in {(root.parent / "shared-go").resolve(), (root.parent / "iris-client-go").resolve()}:
        continue
    print(path)
'
)
for module in "${owned_modules[@]}"; do
  echo "govulncheck ${module}"
  (cd "${module}" && govulncheck ./...)
done
