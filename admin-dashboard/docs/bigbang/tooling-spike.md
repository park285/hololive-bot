# T02 생성기 호환성 시험

2026-09-09. `DEC-20260909-hololive-admin-bigbang-replacement`, T02의 선행 spike입니다. [기계 판독 결과](tooling-spike.json)와 [검증 fixture](contract-compatibility-fixtures.json)는 실제 로컬 시험에서 기록했습니다. 전체 계약 이관, Go DTO 대조, candidate SDK/validator 연결, V02/G02/G09 완료를 의미하지 않습니다.

## 결과

| 도구·시험 | 관찰 |
|---|---|
| `swagger-typescript-api@13.12.6` | 소유 template에서 생성 시 transport를 주입. members GET·room name POST·calendar query를 fake transport가 정확히 3회 호출. 큰 문자열 ID와 한글 보존 |
| SDK 재생성 | 같은 spec·template로 2회 생성한 Admin.ts/data-contracts.ts/http-client.ts가 byte 단위 일치 |
| `ajv@8.20.0` + `ajv-formats@3.0.1` | draft 2020-12 standalone ESM 생성과 fixture 11개 통과. coerceTypes/useDefaults/removeAdditional=false이며 입력 객체 변형 없음 |
| fixture 범위 | oneOf/null·required·추가 속성 거부·큰 ID 문자열·Unicode 길이·date-time·prefixItems/items·unevaluatedProperties |
| 동적 코드 생성 금지 | bundle 소비 프로세스를 `node --disallow-code-generation-from-strings`로 실행하여 11개 통과 |
| compiler/runtime 구분 | bundle 입력에 compiler 없음. `ajv/dist/runtime/ucs2length.js`와 `ajv-formats/dist/formats.js` helper 포함 |
| `@playwright/test@1.63.0` | package만 격리 설치. browser 다운로드·세 엔진 실행은 NOT RUN |
| 취약점·provenance | 격리 npm lock의 9 packages에 `npm audit --json` exit 0, 알려진 취약점 0. registry.npmjs.org resolved/integrity와 license를 결과에 보존 |

JSON Schema의 서버 검증을 브라우저 validator가 대신하지 않습니다(ASVS 2.2.2). fixture의 통과는 ASVS 전체 준수 또는 production CSP의 브라우저 실행 검증이 아닙니다. standalone은 컴파일러 실행을 빌드 시점으로 옮기며 필요한 helper까지 자동으로 없애지는 않습니다. [Ajv standalone 공식 문서](https://ajv.js.org/standalone.html).

## 실행과 승인 경계

별도 `/tmp/hololive-admin-contract-spike.*` package에서 다음 정확한 버전으로 설치했습니다. 프로젝트의 package.json/package-lock.json 및 runtime import graph는 수정하지 않았습니다.

```sh
corepack npm install --prefix <spike-dir> --ignore-scripts --no-audit --no-fund
node <spike-dir>/compatibility.mjs
node <spike-dir>/sdk.mjs
corepack npm audit --prefix <spike-dir> --json
```

spike package의 직접 devDependencies는 `ajv: 8.20.0`, `ajv-formats: 3.0.1`, `@playwright/test: 1.63.0`입니다. 설치된 frontend의 기존 swagger-typescript-api와 esbuild를 사용했고 출력은 임시 디렉터리에만 생성했습니다. 검사 스크립트·fixture·bundle·lock의 SHA-256은 결과 JSON에 기록했습니다. 이 임시 경로는 canonical `generate:api`나 production 코드의 대체 경로가 아닙니다. 정식 T02 구현에서는 테스트와 template를 owning 생성 경로에 통합해야 합니다.

원본 standalone은 10,962 bytes, fixture용 browser bundle은 30,970 bytes입니다. 이는 실제 전체 API candidate나 HTTP 압축 전송량이 아니므로 V06 예산 판정에 사용하지 않습니다.

**사용자 승인:** 위 두 helper의 브라우저 bundle 포함 질문에 사용자가 “승인 ㄱㄱ”라고 답했습니다. `ajv@8.20.0`과 `ajv-formats@3.0.1`의 runtime helper 포함, 해당 버전 고정과 필요한 생성·검증 연결에 적용합니다. 컴파일러는 빌드 전용으로 제한하며 MFA·배포·secret 변경 승인은 포함하지 않습니다. 이 범위의 정식 연동 결과는 [T02 구현 검증](contract-implementation.md)에 기록했습니다.

spike 이후 전체 operation의 생성·검증, Go DTO 대조, canonical generator와 CI/Docker consumer 이관을 완료했습니다. 실제 결과와 남은 출시 검증은 [T02 구현 검증](contract-implementation.md)을 따릅니다. 이 문서는 선행 spike 당시의 범위와 결과를 보존합니다. Fallback delta: none.
