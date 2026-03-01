# hololive-rs Logging Policy

## 목적

- 운영 중 장애 분석 가능성 확보
- `backend-guide`의 `Q2`(구조화 로그, 민감정보 마스킹) 준수
- K8s 운영: **stdout → Fluent Bit → Loki** SSOT 단일 경로

## 정책

1. **구조화 로그**
   - 다른 봇과 동일하게 text 기반 key=value 스타일 사용
   - stdout 전용 출력 (파일 로깅 제거됨)

2. **로그 레벨**
   - 기본 레벨: `info`
   - 환경변수 `RUST_LOG`가 있으면 우선 적용
   - 출력 표기는 기존 Go 봇과 동일하게 `INF/WRN/ERR/DBG/TRC` 약어 사용
   - 타임스탬프는 KST(`+09:00`) 기준으로 기록

3. **로그 수집 경로**
   - stdout → Fluent Bit (DaemonSet) → Loki → Grafana
   - kubectl logs: kubelet 버퍼 (보조)
   - 파일 로깅: 제거됨 (`*__LOGGING__FILE_ENABLED=false`)

4. **로그 조회**
   - Grafana: `http://localhost:30090` (Loki 데이터소스)
   - CLI: `./scripts/logs/tail.sh <service>`, `./scripts/logs/query.sh <service>`
   - Loki 라벨: `job=fluent-bit`, `namespace_name`, `pod_name`, `container_name`

5. **시간대 통일**
   - 로그 timestamp는 KST(`+09:00`) 고정
   - scheduler 관련 시간 필드는 `next_run_kst`, `scheduled_run_kst`, `failed_at_kst` 키 사용

6. **OpenTelemetry 통합**
   - `OTEL_ENABLED=true`일 때 OTLP gRPC exporter 활성화
   - `OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SAMPLE_RATE` 반영
   - tracing layer를 통해 HTTP/scheduler span을 Jaeger로 export

7. **민감정보 보호**
   - 비밀번호, 토큰, 쿠키, API key를 로그에 직접 출력 금지
   - URL 로그 시 query string/secret 포함 여부를 검토 후 기록

## 설정 키

- `SCRAPER__LOGGING__LEVEL` (기본: `info`)
- `SCRAPER__LOGGING__FILE_ENABLED` (기본: `false`, k8s: `false` 고정)
- `ALARM__LOGGING__LEVEL` (기본: `info`)
- `ALARM__LOGGING__FILE_ENABLED` (기본: `false`, k8s: `false` 고정)
- `RUST_LOG` (선택, `tracing` filter override)
- `OTEL_ENABLED` (기본: `false`)
- `OTEL_SERVICE_NAME` (기본: `hololive-rs`)
- `OTEL_EXPORTER_OTLP_ENDPOINT` (기본: `jaeger:4317`)
- `OTEL_EXPORTER_OTLP_INSECURE` (기본: `false`)
- `OTEL_SAMPLE_RATE` (기본: `1.0`)
