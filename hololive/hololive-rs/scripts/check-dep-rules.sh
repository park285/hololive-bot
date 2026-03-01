#!/usr/bin/env bash
# shared-* crate 간 상호 의존 감지 시 실패 (shared-core, shared-infra 제외 — 인프라 계층)
# cargo metadata의 resolve 필드를 파싱
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$WORKSPACE_DIR"

# cargo metadata로 의존성 그래프 추출
METADATA=$(cargo metadata --format-version 1 --no-deps 2>/dev/null)

# shared-* 패키지 ID 목록 (shared-core, shared-infra 제외)
SHARED_PKGS=$(echo "$METADATA" | python3 -c "
import json, sys
data = json.load(sys.stdin)
INFRA = {'shared-core', 'shared-infra'}
shared_names = set()
for pkg in data['packages']:
    if pkg['name'].startswith('shared-') and pkg['name'] not in INFRA:
        shared_names.add(pkg['name'])
print('\\n'.join(sorted(shared_names)))
")

# 각 shared-* 패키지의 의존성에 다른 shared-*가 있는지 검사 (shared-core, shared-infra 허용)
VIOLATIONS=$(echo "$METADATA" | python3 -c "
import json, sys
data = json.load(sys.stdin)
INFRA = {'shared-core', 'shared-infra'}
shared_names = set()
for pkg in data['packages']:
    if pkg['name'].startswith('shared-') and pkg['name'] not in INFRA:
        shared_names.add(pkg['name'])

violations = []
for pkg in data['packages']:
    if pkg['name'] in shared_names:
        for dep in pkg.get('dependencies', []):
            if dep['name'] in shared_names and dep['name'] != pkg['name']:
                violations.append(f\"  {pkg['name']} -> {dep['name']}\")

if violations:
    print('ERROR: shared-* inter-dependency violations found:')
    for v in violations:
        print(v)
    sys.exit(1)
else:
    print('OK: No shared-* inter-dependency violations.')
")

echo "$VIOLATIONS"
