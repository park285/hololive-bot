# hololive-rs Logging Policy

## 목적

- 운영 중 장애 분석 가능성 확보
- `backend-guide`의 `Q2`(구조화 로그, 민감정보 마스킹) 준수
- K8s 운영 기본값을 stdout-only로 통일

## 정책

1. **구조화 로그**
   - 다른 봇과 동일하게 text 기반 key=value 스타일 사용
   - 기본은 stdout 출력
   - `*_LOGGING__FILE_ENABLED=true`일 때만 파일(`service + combined`) 동시 기록

2. **로그 레벨**
   - 기본 레벨: `info`
   - 환경변수 `RUST_LOG`가 있으면 우선 적용
   - 출력 표기는 기존 Go 봇과 동일하게 `INF/WRN/ERR/DBG/TRC` 약어 사용
   - 타임스탬프는 KST(`+09:00`) 기준으로 기록

3. **로그 경로**
   - stdout-only 모드: 파일 경로 미사용
   - file 모드:
     - 컨테이너 내부: `/app/logs/hololive-scraper.log`
     - 통합 로그: `/app/logs/combined.log`

4. **회전 정책**
   - K8s prod 기본은 stdout-only (파일 회전 불필요)
   - file 모드 사용 시:
     - 기본 단일 파일: `hololive-scraper.log`
     - 통합 파일: `combined.log`
     - 필요 시 외부 logrotate/system 수준에서 순환

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
- `SCRAPER__LOGGING__FILE_ENABLED` (기본: `false`)
- `SCRAPER__LOGGING__DIR` (기본: `logs`)
- `SCRAPER__LOGGING__FILE` (기본: `hololive-scraper.log`)
- `SCRAPER__LOGGING__COMBINED_FILE` (기본: `combined.log`)
- `ALARM__LOGGING__FILE_ENABLED` (기본: `false`)
- `RUST_LOG` (선택, `tracing` filter override)
- `OTEL_ENABLED` (기본: `false`)
- `OTEL_SERVICE_NAME` (기본: `hololive-rs`)
- `OTEL_EXPORTER_OTLP_ENDPOINT` (기본: `jaeger:4317`)
- `OTEL_EXPORTER_OTLP_INSECURE` (기본: `false`)
- `OTEL_SAMPLE_RATE` (기본: `1.0`)
