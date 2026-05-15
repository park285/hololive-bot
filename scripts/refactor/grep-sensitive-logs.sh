#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
source "${ROOT_DIR}/scripts/ci/go-workspace-modules.sh"

mapfile -t TARGETS < <(go_workspace_runtime_log_scan_targets)

pattern='logger\.(Info|Warn|Error|Debug)|sharedlog\.(Info|Warn|Error|Debug)|slog\.'

bad=0
for target in "${TARGETS[@]}"; do
  if [[ ! -d "$target" ]]; then
    continue
  fi

  while IFS= read -r line; do
    if echo "$line" | grep -Eiq 'slog\.(String|Any)\("(raw|payload|body|prompt|response|authorization|cookie|token|api_key|apikey|client_secret|password)"'; then
      echo "suspicious sensitive log: $line" >&2
      bad=1
    fi
  done < <(grep -RInE "$pattern" "$target" --include='*.go' --exclude='*_test.go' || true)
done

while IFS= read -r line; do
  echo "unsafe llm provider error log: $line" >&2
  bad=1
done < <(grep -RInE 'sharedlog\.ErrorAttrs\(|slog\.(String|Any)\("error",[[:space:]]*err\.Error\(\)' hololive/hololive-llm-sched/internal/llm --include='*.go' --exclude='*_test.go' || true)

while IFS= read -r line; do
  echo "unsafe llm provider diagnostic pattern: $line" >&2
  bad=1
done < <(grep -RInE 'refusal=%s|raw=%q|RawJSON\(' hololive/hololive-llm-sched/internal/llm --include='*.go' --exclude='*_test.go' || true)

if [[ "$bad" -ne 0 ]]; then
  exit 1
fi

echo "ok: no obvious sensitive log fields"
