# PostgreSQL extension experiments

kapu의 새 합성 DB만 사용한다. 운영 접속 옵션, 실제 데이터 복제, 포트 공개는 없다.
컨테이너는 실행별 고유 이름·tmpfs·자원 제한을 사용하고 종료 시 해당 컨테이너만 제거한다.

## 계측 비용 재현

```bash
docker buildx build --platform linux/amd64 --target runtime \
  --build-arg REVISION="$(git rev-parse HEAD)" --provenance=false --sbom=false \
  --load --tag hololive-postgres:extension-experiment deploy/images/postgres
python3 -B scripts/experiments/postgres-extensions/benchmark.py \
  --image hololive-postgres:extension-experiment --seconds 8 --output /tmp/pg-extension-results.json
```

PostgreSQL 18.6/Alpine, kapu amd64, 컨테이너 CPU 2개·메모리 1536MiB,
pgbench prepared mode·4 clients·2 threads. workload마다 2초 예열·8초 측정하며
`baseline → kcache → wait_sampling → both → both → wait_sampling → kcache → baseline`
순서로 실행한다. baseline에도 기존 `pg_stat_statements`를 켠다.

2026-10-07 두 회차 평균 TPS와 baseline 대비 변화:

| Workload | baseline | kcache | wait_sampling | both |
|---|---:|---:|---:|---:|
| 600개 target 집계 | 8,243.19 | 8,194.19 (-0.59%) | 8,243.07 (-0.00%) | 8,096.03 (-1.79%) |
| 큰 timestamp 배열 조회 | 67.79 | 67.73 (-0.10%) | 67.58 (-0.32%) | 67.80 (+0.00%) |
| 짧은 행 잠금·갱신 | 12,060.23 | 11,560.38 (-4.14%) | 11,988.29 (-0.60%) | 11,624.49 (-3.61%) |

| Workload | baseline 평균/p95/p99 (ms) | both 평균/p95/p99 (ms) |
|---|---:|---:|
| target 집계 | 0.4820 / 0.3550 / 0.4565 | 0.4911 / 0.3590 / 0.4725 |
| 큰 배열 | 58.9312 / 86.0390 / 89.1540 | 58.9303 / 86.1275 / 90.1785 |
| 갱신 | 0.3298 / 0.2120 / 0.2765 | 0.3422 / 0.2190 / 0.2825 |

p95/p99는 회차별 분위수를 평균한 값이다. 24개 측정 구간 모두 실패 transaction은 0건이었다.
kcache는 쿼리 CPU를 수집했고 wait sampling은 큰 배열에서 `ClientWrite`, 갱신에서
`Lock/transactionid`와 `LWLock/BufferContent`를 관측했다. 짧은 집계에서 표본이 0인 것은
그 구간에 수집된 대기 표본이 없다는 뜻이다.

이는 짧은 합성 부하의 비용 추정이다. 두 회차만으로 1% 안팎 차이를 유의한 차이라고
판정할 수 없다. pgbench는 배열을 text로 받으므로 앱 pgx의 binary 전송과 절대 지연이 다르다.
tmpfs 실험은 실제 디스크 I/O를 대표하지 않는다. ARM64는 kapu BuildKit 에뮬레이션에서
기능을 검증했으며 ARM64 실기 성능을 측정한 것은 아니다. 운영 CPU·I/O·메모리의
전후 변화는 승인된 적용 뒤 같은 관측 구간의 차분으로 확인해야 한다.

## JSON Schema 실험

```bash
docker buildx build --platform linux/amd64 \
  --file scripts/experiments/postgres-extensions/Dockerfile.jsonschema \
  --provenance=false --sbom=false --load --tag hololive-postgres-jsonschema:experiment \
  scripts/experiments/postgres-extensions
python3 -B scripts/experiments/postgres-extensions/jsonschema.py \
  --image hololive-postgres-jsonschema:experiment --output /tmp/pg-jsonschema-results.json
```

[pg_jsonschema v0.3.4](https://github.com/supabase/pg_jsonschema/releases/tag/v0.3.4)의
공식 GNU/amd64 바이너리와 별도 PostgreSQL 18.6/Bookworm 이미지를 사용한다.
운영 Alpine 호환성이나 ARM64 동작을 입증하는 실험이 아니다. production Dockerfile에
pg_jsonschema를 넣지 않는다.

운영 `members.aliases`의 기존 CHECK와 동일하게 `ko`/`ja` 배열을 요구하고,
SQL NULL·추가 키·배열 원소 타입 허용을 유지했다. 14개 경계 사례와 실제 CHECK의
정상/거부 INSERT를 통과했다. 20,000행에 대한 네 회차 실행 시간 중앙값은
내장 SQL **6.243ms**, pg_jsonschema **146.845ms**로 약 **23.5배**였다.
절대 추가 비용은 약 7µs/행이므로 저빈도 쓰기에서 이 배수만으로 도입을 거부할 근거는 아니다.
다만 현재의 단순 계약을 그대로 옮겨서는 표현력 이득이 작고 Alpine 배포 검증이 추가로 필요하다.
복잡한 다른 스키마로 이 성능 결과를 일반화하지 않는다.

추가로 기존 coverage CHECK 형태는 키가 없을 때 SQL NULL이 되어 통과하고,
JSON Schema의 `required`는 이를 거부함을 재현했다. 이런 전환은 계약 강화다.
실데이터 위반 존재를 입증한 것은 아니며, 별도 앱·데이터 검토 없이 교체하지 않는다.

## 채택 판단

- `pg_stat_kcache` 2.3.2와 `pg_wait_sampling` 1.1.11: 선택적 운영 계측 후보로 준비한다.
  [적용·복구 절차](../../../docs/current/runbooks/postgres-observability.md)를 따른다.
- `pg_jsonschema`: 이번에는 보류한다. 반복되는 복잡한 JSON 계약이 생길 때 source build의
  Alpine/ARM64 검증과 쓰기 비용을 함께 다시 평가한다.
- `pg_qualstats`/HypoPG, `pg_ivm`: 이번 이미지에 추가하지 않는다. 인덱스 선택이나
  집계 유지 비용이라는 별도 가설을 검증할 때 필요한 범위로 평가한다.

확장 원본과 버전별 설정은 [pg_stat_kcache REL2_3_2](https://github.com/powa-team/pg_stat_kcache/tree/REL2_3_2),
[pg_wait_sampling v1.1.11](https://github.com/postgrespro/pg_wait_sampling/tree/v1.1.11)를 따른다.
