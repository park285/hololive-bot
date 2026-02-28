# Library Adoption Rollout Report (2026-02-24)

## 1) 변경 커밋

- Repository: `hololive-scraper-rs` (monorepo path)
- Commit: `644f7f933`
- Message: `feat(scraper-rs): complete library adoption backlog and hardening`
- Commit date (KST): 2026-02-24

## 2) 이미지 빌드/반영

실행 순서(2026-02-24 KST):

```bash
docker compose -f docker-compose.prod.yml build hololive-scraper
docker compose -f docker-compose.prod.yml build hololive-alarm
docker compose -f docker-compose.prod.yml up -d hololive-scraper hololive-alarm
```

빌드된 이미지:

- `hololive-scraper-rs:prod` → `sha256:2eeb6b9563935cd8984eaa7d42996be27ab9d5918e1fc883ded91e7d74c184d0`
- `hololive-alarm:prod` → `sha256:76a829a2433427b31515361f49ccc3028ed9930c017ea0683c72aa828bfd625b`

컨테이너 재기동 시각(UTC):

- `hololive-scraper-rs`: `2026-02-23T23:04:28.032867069Z`
- `hololive-alarm`: `2026-02-23T23:04:28.022375225Z`

## 3) 검증

정적/테스트 게이트:

```bash
cargo fmt --all --check
cargo clippy --workspace -- -D warnings
cargo test --workspace
```

결과: PASS

런타임 헬스 점검:

```bash
curl http://localhost:30010/health
curl http://localhost:30011/health
curl http://localhost:30011/ready
```

결과: PASS (`status=ok/alive`, scheduler healthy)

## 4) 비고

- 초기 `docker compose up -d --build hololive-scraper hololive-alarm` 시, BuildKit cache 경쟁으로
  `trust-dns-resolver` unpack 오류(`.cargo-ok File exists`)가 1회 발생했습니다.
- 서비스별 순차 build(`build hololive-scraper` → `build hololive-alarm`)로 재시도 후 정상 반영했습니다.
