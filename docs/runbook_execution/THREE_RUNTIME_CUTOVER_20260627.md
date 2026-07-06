# Three-Runtime Cutover 실행 기록 — 2026-06-27

5→3 runtime 통합의 **production 컷오버** 한 건에 대한 전체 범위 기록.
리뷰어가 준비·실행·장애·수정·복구·검증·git 반영을 완전히 재구성할 수 있도록 작성한다.

대상 호스트: central main host (`/run/hololive-bot/` 존재, tailnet `100.100.1.3`).
실행자: interactive session (git writer). remote-control daemon stopped.

---

## 1. 작업 정의와 범위

**컷오버 = 구 per-runtime 컨테이너 3개를 통합 `hololive-api` 1개로 교체하는 production 전환.**

| 구분 | 컨테이너 | 처리 |
|---|---|---|
| 제거 대상 | `hololive-kakao-bot-go` (bot) | 제거 |
| 제거 대상 | `hololive-admin-api` (admin) | 제거 |
| 제거 대상 | `hololive-llm-scheduler` (llm) | 제거 |
| 신규 | `hololive-api` (bot/admin/llm 단일 프로세스) | 생성 |
| 영향 없음 | `hololive-alarm-worker` | 재생성(동일 역할 유지) |
| 영향 없음 | `hololive-youtube-producer-c` | AP 서비스, 컷오버 대상 아님 |
| 인프라 | `holo-postgres` / `valkey-cache` | 1차 실행 시 compose 정의 변경으로 Recreated. 스크립트가 보장하는 건 target(`hololive-api`) up 뿐이고, postgres/valkey 재생성은 `up <target>`의 depends_on 부수효과. 데이터는 named volume(`holo-pg-data`/`valkey-cache-data`)으로 보존 |

컷오버 작업 단위에 포함된 것: 사전 조사, 안전 검증, 1차 실행, 장애 진단, 코드 수정(#149), 2차 실행, 복구 검증, git 반영.

## 2. 전제조건 — env_file P0 (PR #148, 선행 blocker)

컷오버 직전 `/cross-debate` 검증에서 codex 패널이 단독으로 발견한 P0가 선행 차단 요소였다. 이것이 해소되지 않으면 컷오버 명령 자체가 preflight에서 죽는다.

- 결함: 통합이 env_file 기본값을 `…/bot.env`(존재) → `…/hololive-api.env`(부재)로 변경. OpenBao(`bot.env`만 렌더)·boot wrapper와 미정렬.
- 수정(#148, main `e0a5b03f`): `docker-compose.prod.yml:421` / `docker-compose.live-compat.yml:33` 기본값을 `bot.env`로 복원 + 계약 테스트/README 정렬.
- 검증: production-identical `config --quiet` exit 1 → exit 0.

자세한 내용은 PR #148 및 별도 cross-debate 기록 참조. 본 문서는 #148 머지 이후의 컷오버 실행을 다룬다.

## 2.5. 선행 하드닝 — #146 / #147 (컷오버 전 별도 안정화)

env_file P0(#148) 외에도, 통합 머지(#145, main `803a9841`) 직후 컷오버 전에 **두 건의 안정화가 별도로 선행 완료**됐다. 리뷰어가 이 두 PR을 컷오버 실행과 분리해 인지하도록 명시한다.

| PR | main 커밋 | 내용 |
|---|---|---|
| #146 | `e45a469a` | three-runtime 통합 후 빌드/마이그레이션 경로 정리: Dockerfile dead-copy 제거, migration mount 경로 정정, `check-removed-runtime-build-paths.sh` guard 추가(삭제된 옛 모듈 경로 참조 회귀 차단, 이후 `check-removed-runtime-regressions.sh`로 통합), ci-boundary gate 보강. docker build 3종·compose render·migration mount·ci-boundary 447 PASS로 검증 |
| #147 | `21924a09` | three-runtime 후속 NFR 하드닝: 미사용 docker-proxy grant 제거(보안), build-only compose env fallback(DX), youtube-producer profile·배포후 모니터링 문서. 무효 확인 항목(llm `/ready`·alarm `/health` liveness-only, `HOLO_BOT_URL` 기존 alias)은 무변경 |

즉 컷오버 시점의 main은 `#145 → #146 → #147 → #148`까지 누적된 상태였고, 본 문서의 §5~§8은 그 위에서 일어난 #149 컷오버 실행 기록이다.

## 3. 준비 단계 — 조사

컷오버 메커니즘을 추측 없이 확정하기 위해 다음을 조사했다.

1. **현재 LIVE 컨테이너**: `docker ps` — 구 bot/admin/llm 3개가 healthy 가동 중 확인.
2. **systemd 단위**: `hololive-compose.service`(oneshot, `systemd-compose-up.sh` 호출)가 boot 경로. OpenBao agent(`openbao-agent-hololive-bot.service`)가 env/cert 렌더.
3. **정식 컷오버 명령** (`docs/current/runbooks/release.md`):
   ```bash
   sudo -n env COMPOSE_FILE=deploy/compose/docker-compose.prod.yml:deploy/compose/docker-compose.live-compat.yml \
     COMPOSE_ENV_FILE=/run/hololive-bot/compose.env \
     ./scripts/deploy/compose-redeploy-service.sh hololive-api
   ```
4. **스크립트 동작** (`compose-redeploy-service.sh`): ① `config --quiet` preflight → ② `build hololive-api` → ③ `removed_runtime_cleanup_before_cutover`(구 컨테이너 `stop`+`rm -f`) → ④ `up -d --no-build hololive-api` → ⑤ `ps`.
   - **중요 리스크 발견**: 이 스크립트에는 **health-gate도 자동 rollback도 없다**. `up -d`는 healthy를 기다리지 않으므로 컷오버 후 health 검증과 실패 시 롤백을 실행자가 직접 통제해야 한다.
   - `removed_runtime_cleanup_before_cutover`는 구 컨테이너를 `rm -f` 하므로 롤백이 단순 재시작이 아니라 재생성이다.

## 4. 사전 안전 검증 (실측)

| 항목 | 확인 방법 | 결과 |
|---|---|---|
| (A) Migration 파괴성 | `git log` migrations + DB 가동 이력 | 통합 PR은 신규 migration 없음(`a04f3c54`는 경로 이동만), prod DB 35h+ 가동 → `bootstrap-and-apply.sh` idempotent no-op |
| (B) H3 SNI/SAN | `openssl x509 -ext subjectAltName` + compose.env SNI 키 | cert SAN=`IP 100.100.1.3`, 필요 SNI(`HOLOLIVE_H3_SERVER_NAME`/`HOLOLIVE_INTERNAL_H3_SERVER_NAME`)=`100.100.1.3` → SAN 커버. (cross-debate §10 deferred 항목 실측 해소) |
| (C) 이미지 | `docker images` | `hololive-api:prod`/`alarm-worker:prod`/`youtube-producer:prod` 존재 |
| (D) 롤백 기준점 | `docker images` grep | 구 이미지 `hololive-kakao-bot-go:prod`/`hololive-admin-api:prod`/`hololive-llm-scheduler:prod` 로컬 보존 확인 |

## 5. 컷오버 1차 실행 → 실패

§3의 정식 명령 실행. `REDEPLOY_EXIT=0`으로 스크립트는 정상 종료했고:
- 구 3개(`hololive-kakao-bot-go`, `hololive-admin-api`, `hololive-llm-scheduler`) 제거됨
- `hololive-db-migrate` Started→Exited (idempotent no-op, 정상)
- `hololive-api` Created→Started (image `hololive-api:prod`)

그러나 직후 health-gate 폴링에서 **crash loop** 확인:
```
status=restarting  health=unhealthy  RestartCount>0
logs: prepare log file failed: stat log file failed:
      lstat /app/logs/hololive-api.log: permission denied (반복)
```
이 시점 구 서비스는 이미 제거되어 **봇이 다운 상태**.

## 6. 장애 진단 — 근본 원인

| 컨테이너 | user | logs mount |
|---|---|---|
| `hololive-api` | **65532:65532** | `…/logs` (host) |
| `hololive-alarm-worker` | 1000:1000 | `…/logs` (host) |
| host `logs/` 디렉터리 | 소유 `1000:1000`, 권한 **0750** | — |

uid 65532는 others에 해당하고 디렉터리가 `0750`(others 권한 0)이라 `hololive-api.log`를 생성할 수 없어 크래시. 구 bot/admin/llm·alarm-worker는 전부 uid 1000이었다. Dockerfile USER 지시 비교로 확정:
- `hololive-api/Dockerfile`: `USER 65532:65532` (통합 시 회귀)
- `hololive-alarm-worker/Dockerfile`, 구 `hololive-kakao-bot-go/Dockerfile`(`35dcf0f8^`): `USER 1000:1000`

즉 통합 Dockerfile만 distroless nonroot 기본 uid(65532)로 작성된 것이 직접 원인.

## 7. 수정 + 2차 컷오버 (PR #149)

근본 수정: `hololive-api/Dockerfile`의 user를 alarm-worker와 동일한 `1000:1000`으로 통일 (passwd/group, `COPY --chown`, `USER` 3곳, +4/−4). 임시 compose override가 아니라 Dockerfile을 고쳐 재발(다른 호스트·CI 빌드) 차단.

```diff
-RUN printf 'app:x:65532:65532:app:/tmp:/sbin/nologin\n' …  / 'app:x:65532:\n'
+RUN printf 'app:x:1000:1000:app:/tmp:/sbin/nologin\n' …    / 'app:x:1000:\n'
-COPY --from=builder --link --chown=65532:65532 /dist ./
+COPY --from=builder --link --chown=1000:1000 /dist ./
-USER 65532:65532
+USER 1000:1000
```

재빌드 + 재배포(§3 명령 재실행, `removed_runtime_cleanup`은 구 컨테이너 부재로 no-op). `REDEPLOY_EXIT=0`.

## 8. 복구 검증 (실측)

| 검증 | 결과 |
|---|---|
| 컨테이너 상태 | `status=running health=healthy RestartCount=0` (이후 23분+ 안정 유지) |
| logs 쓰기 | `logs/hololive-api.log` uid 1000으로 정상 기록 |
| permission denied 재발 | 없음 |
| 3 plane 기동 | bot `:30001`("Bot started successfully", "Iris server connected", "Valkey connected") / admin `:30006` / llm(스케줄러 전부 waiting) |
| alarm-worker 연동 | 정상 디스패치, hololive-api 연동 에러 없음 |
| 구 컨테이너 | bot/admin/llm 3개 완전 제거 확인 |
| 전체 스택 | postgres/valkey/alarm-worker/youtube-producer-c 전부 healthy |

## 9. git 반영

| repo | 커밋 | 내용 |
|---|---|---|
| hololive-bot | `135e6b8f` (#149) | Dockerfile uid 1000 통일 (squash 머지) |
| meta-repo | `7ee647c` | bump hololive-bot → `135e6b8f34df` |

#149는 pre-push gate 통과 후 PR → squash 머지 → submodule FF → meta-repo 재bump. production 로컬 이미지(uid 1000)와 main Dockerfile이 일치.

**main tip 정정**: #149의 squash 머지 커밋은 `135e6b8f`이지만, 그 직후 **본 컷오버 실행 기록 문서 커밋이 추가되어 hololive-bot main tip = `f34af5a2`**(`docs(runbook): three-runtime 컷오버 실행 기록 추가`)다. 즉 `135e6b8f`는 코드(Dockerfile uid) 마지막 변경점이고, `f34af5a2`는 본 문서를 포함한 현재 main tip이다 — 둘을 혼동하지 말 것. meta-repo bump(`7ee647c`)는 `135e6b8f` 시점 SHA를 가리키며, 문서 커밋 반영 시 별도 재bump가 필요하다.

## 10. 롤백 절차 (미사용, 참고)

컷오버 실패 시 절차(`docs/current/runbooks/rollback.md` + 본 작업 기준):
1. 구 이미지 3종은 로컬 보존됨(`…:prod`).
2. 구 compose 정의는 `35dcf0f8^`에 있음 — 구 3개 서비스 복원은 단순 재시작이 아니라 그 정의로 재생성 필요(`removed_runtime_cleanup`이 `rm -f` 했으므로).
3. 본 컷오버는 fix-forward(#149)로 해소되어 롤백은 실행하지 않았다.

## 11. 후속 / 권장

### 외부 리뷰가 식별한 follow-up

**P1 (운영 안전 — 우선 처리)**

- **health-gate 부재**: `compose-redeploy-service.sh`는 `up -d` 후 healthy를 기다리지 않고 자동 rollback도 없다. 구 컨테이너를 `rm -f` 한 뒤 통합 컨테이너 health를 보장하지 않아 이번 crash-loop 때 봇 다운이 발생했다. `--wait`/health 폴링 + 실패 시 자동 abort를 후속 추가. (코드 변경 영역 — 본 문서 scope 밖)
- **rollback playbook**: 5→3 cutover 전용 emergency rollback이 `docs/current/runbooks/rollback.md`에 추가됨(구 compose `35dcf0f8^` export, 구 이미지 재생성, `--no-deps`로 postgres/valkey 보존, schema 호환 명시). **(반영 완료)**
- **host uid preflight**: 컷오버 직접 원인이던 host `logs/`(uid 1000:1000, 0750) vs 컨테이너 uid 불일치를 컷오버 전에 검사하는 preflight(예: 컨테이너 USER와 mount 대상 디렉터리 소유/권한 대조)가 없다. #149로 Dockerfile은 uid 1000으로 고정됐으나, preflight 가드는 미구현 — 후속 대상.
- **bot 실응답 확인**: 실시간 KakaoTalk 메시지로 webhook→reply 왕복을 최종 확인해야 한다(코드/로그 레벨은 정상). live 검증 항목으로 남음.

**P2 (개선)**

- **metrics 구체화**: `runbooks/hololive-api.md` Metrics 절을 실제 운영 명령(docker stats/inspect, `/metrics` X-API-Key scrape, pg_stat_activity, valkey slowlog)과 경계값으로 구체화함. plane별 pg 구분 불가(application_name 미설정) 사실 명시. **(반영 완료)**
- **`.dockerignore`**: 빌드 컨텍스트 축소/재현성용 `.dockerignore` 점검 — 후속.
- **`API_SECRET_KEY` fail-fast**: prod에서 `API_SECRET_KEY:-` 빈 값 fallback이 metrics/auth 경계를 조용히 약화시킬 수 있다. 빈 값일 때 기동 단계에서 fail-fast 검토 — 후속(코드 영역).
- **guard test 제외 축소**: `check-removed-runtime-build-paths.sh`(#146, 현 `check-removed-runtime-regressions.sh`에 통합) 등 guard의 test 제외 범위를 최소화 — 후속.

### 상시 관찰 / 교훈

- **컷오버 후 24h 관찰** (`runbooks/hololive-api.md`): 단일 프로세스라 동시 spike(LLM digest + admin stats + bot 렌더 동시), GC, plane별 DB pool 합산, deunhealth restart count 모니터링. 경계값은 해당 runbook 참조.
- **교훈**: 세 결함(env_file, uid, 그리고 후속에서 드러난 운영 gap) 중 두 P0(env_file, uid)는 `go build`/`docker build`를 모두 통과하고 production 런타임 경로에서만 드러났다. 통합류 작업은 빌드/테스트 CI로 불충분하며 **실제 호스트 컷오버 + health 검증이 필수**다.
