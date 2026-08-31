# hardening-evidence

> Historical verification capture. Do not use as a current CI or runtime contract.

YouTube collector hardening PR의 reviewer evidence 템플릿입니다. schema와 경로 계약만 고정합니다. 생성 log, command 결과, 빈 placeholder file은 커밋하지 않습니다.

존재하지 않는 범위의 empty file을 만들지 말고, 해당 PR의 `manifest.json`에 `not_applicable`로 표시합니다.

## Directory

```text
hardening-evidence/
  manifest.json
  traceability.json
  commands.jsonl
  tests/
    go-default.jsonl
    go-race.log
    go-sonic.jsonl
    node.log
    shared-source-observation.jsonl
  sql/
    exact-target-explain.json
    projection-target-explain.json
    publish-fault-matrix.json
    migration-177-compatibility.json
    failure-diagnostic-transition-matrix.json
  rendered/
    central-collector.json
    live-collector.json
    osaka-a-collector.json
    seoul-b-collector.json
    osaka2-d-collector.json
    host-native-env.keys.txt
  artifacts/
    go-manifest.json
    node-manifest.json
    image-digest.txt
    sha256sums.txt
  scans/
    secrets.log
    hardening-contract.log
    markdown-contract.log
```

## Manifest

`manifest.schema.json`은 contract v6 §23.3 evidence manifest(`schema_version` 2)를 고정합니다.

- `baseline_sha` / `head_sha` / `merge_base_sha`는 40 lowercase hex입니다.
- `document.sha256`은 해당 PR이 사용한 명세 파일의 실제 hash입니다.
- `results`는 log parser 산출값이며 사람이 임의 입력하지 않습니다.
- hardening PR에서 `production_change_performed`는 항상 `false`입니다.
- `results.unexecuted`가 1 이상이면 PR completion은 blocking입니다.

Command record와 traceability JSON의 필드 계약은 같은 문서의 §23.4–§23.5를 따릅니다. screenshot, “테스트 통과” prose, secret 포함 dump, skip을 pass로 합산한 값은 evidence가 아닙니다.
