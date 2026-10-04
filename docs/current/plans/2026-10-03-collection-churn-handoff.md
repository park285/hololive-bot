# YouTube 수집 generation churn·알림 누락 조사 핸드오프

## 목적과 현재 상태

오래된 UPCOMING 영상 재확인이 전체 수집 generation을 반복 교체하여, 무관한 쇼츠·영상·커뮤니티 등의 결과를 폐기하는 문제를 이어서 처리한다. 확대 조사와 독립 검토는 완료했고 **이 문제의 제품 코드 수정은 아직 하지 않았다.** 일반 영상/최초공개 알림의 별도 적격성 결함도 발견했다.

- 사용자는 추가 조사, 관련 다른 세션 작업 인수, `subagent-driven` 방식의 상세·광범위 진행을 요청했다. 마지막 요청은 **다음 세션용 핸드오프 작성**이다.
- 현재 요청만으로 새 구현·계약 변경·migration·배포·운영 SQL·Git 게시 승인이 생기지 않는다. 다음 세션의 사용자 지시와 기존 승인 범위를 구분한다. 과거 SSOT 수정 배포 승인을 이번 문제의 새 배포로 확대하지 않는다.
- 후속 실질 작업도 subagent-driven 방식으로 진행하되, 독립적이고 충분한 크기의 작업만 분할한다. 부모가 범위·공유 계약·통합·최종 검증을 소유한다. 하위 에이전트는 중첩 위임과 작업 중 build/lint/test/formatter를 하지 않는다.
- 조사 중 제품 파일·운영 설정·데이터를 변경하지 않았다. 임시 Go 프로브와 격리 PostgreSQL 컨테이너는 제거했다. 이 문서는 별도 로컬 파일이며 커밋·게시하지 않았다.

## 작업 공간·운영 리비전 — 가장 먼저 구분할 것

| 위치/서비스 | 마지막 확인 상태 | 주의 |
|---|---|---|
| `/home/kapu/work/iris-stack/hololive-bot` | `refactor/api-ownership-20261003`, HEAD `a7316af71…`, 조사 당시 무관한 변경 423개 | 덮기·reset·stash·전체 staging 금지. 미게시 collector refactor와 운영 코드를 혼동하지 않는다. |
| `/home/kapu/work/iris-stack/hololive-ssot-20261003` | `fix/ssot-20261003`, `1942a78afaba45bb830bb63cbff33cdf5ae29e26` | 본 문서 위치. API/collector는 아래 `5a3`와 같고 worker SSOT 후속 수정만 추가된 기준이다. |
| `/home/kapu/work/iris-stack/hololive-db-growth-20261003` | API release `43a5c57a1ad7a3d4aed95f95bc981c9a65688953`, 배포 기록 commit `6d15ada9ba9aad7e3b312613fb08c13abec6b45e` | 조사 중 다른 세션이 retention 수정·API 단독 배포를 완료했다. 보존한다. |
| 중앙 API | **7.2.3 / `43a5c57a1…`**, healthy, 재시작 0 | 2026-10-03 10:23:34 UTC 시작. `5a3` 대비 retention 관련 8개 파일만 변경. |
| 중앙 worker | **`1942a78afaba…`**, healthy, 재시작 0 | 앞선 구독 commit/cache 정합성·관측 조회 시각 수정이 배포됨. 되돌리지 않는다. |
| 중앙 collector c | **`5a3f0377659a0f54d15236f2c6c2a9d49dc09702`**, healthy | 이번 collector 조사·재현의 운영 기준. |

다음 구현 기준은 최신 Git 관계와 runtime을 확인해 정한다. `5a3`만으로 전체 stack을 재배포하면 worker 후속 수정이나 API retention을 누락할 수 있다. 기존 worktree를 무단 변경하지 말고 한 파일에 한 writer 원칙을 유지한다.

프로젝트 지침은 stack·Hololive의 AGENTS, `/home/kapu/work/iris-stack/.agents/workflows.md`, `docs/current/PROJECT_MAP.md`를 따른다. Go 1.27.1 및 언어 스킬을 적용한다. 조사 당시 운영 collector의 계약 패키지는 `internal/runtime/sourceobservation`이다. 기본 checkout의 새 `internal/runtime/collection` 패키지는 운영 `5a3`에 없다.

## 확정한 원인과 실측

### 대상 선정과 전역 무효화

- 예정 시각이 지난 UPCOMING/출처 미상 영상은 재확인 대상이다. 최근 availability 관측(UNKNOWN 포함)이 있으면 빠졌다가 freshness 예산이 지나면 다시 들어온다. UNKNOWN은 종료 사실이 아니다.
- 현재 live poll 120초에서 freshness 예산은 `min(5분, 2×poll+30초)=270초`다. effective/observed clock으로 판정하므로 처리 로그 시각만으로 정확한 재진입 초를 역산하지 않는다.
- 이 출입이 전체 projection generation을 바꾼다. 해당 content 대상이 같아도 snapshot 조회·lease renew·최종 publish가 이전 generation을 거부한다.
- `superseded_release`는 IDLE, `retry_not_before=NULL`, `next_due_at=LEAST(next_due_at,now)`로 만들어 즉시 재획득 가능하다. **cadence/jitter는 우회하지만 RPC limiter는 우회하지 않는다.**

사용자가 확인한 2026-10-03 18:00~18:15 KST 근거는 다시 확인하려고 재실행하지 않는다:
- generation 유지 구간 88개 중 79개가 16초 미만; content 작업 평균 16.1초.
- 쇼츠 결과 폐기 93.7%, 커뮤니티 65.5%, 채널 live 66.7%.
- 최근 24시간 쇼츠 정상 수락 간격 1,460개: 설정 5분, 중앙값 16.1분, p95 48.9분, 최대 114.9분, 10분 초과 82.2%. 이는 업로드→알림 지연이 아니다.

부모가 같은 구간에서 추가 측정:
- content RPC 1회 평균: **rate_limit 대기 7.304초 + helper 0.722초**. videos→shorts 순차 2회면 약 **16.052초**.
- 다른 작업도 대기 7.24~7.29초, helper 0.127~0.829초. 주요 시간은 YouTube 응답이 아니라 호출 제한 대기다.
- helper 처리율 약 **2.001 RPC/s**, 설정 fleet 상한 **2 RPC/s**. 미세 초과는 Prometheus extrapolation 수준이며 제한 우회 증거가 아니다.
- 종류 누락을 보정한 명목 수요는 61개 표본 모두 상한의 **146.03~152.69%**, 연속 900초간 85% 초과. 명목값은 재시도를 포함하지 않는다.
- lease는 provider admission/limiter 대기 전에 획득된다. 대기 동안에도 세대 변경에 노출된다. worker 증설로 호출 상한은 늘지 않는다.

### renew 정체는 별도 경로

실제 운영 코드의 `joblease.Repository.Run`을 격리 PG18에서 `go run -race`로 실행했다. G1의 video/shorts target은 그대로 두고 무관한 video_live_check 하나만 추가해 G2를 활성화했다.
- renew: `FENCE_LOST`, 동일 owner·미만료·동일 content bundle인데 ACTIVE가 남고 새 Acquire는 거부됨.
- snapshot: `ErrProjectionStale`; publish 잠금 함수의 old generation 결과는 0행.
- 별도 publish 사례: `ReleaseSuperseded` 후 즉시 Acquire 성공, epoch 증가.

중앙 c 실설정: workers 4, queue 16, acquisition cadence 1초, TTL 60초, renew 20초/timeout 5초, youtubejs inflight 4.

**측정 해석 정정:** 18:00~18:15 lease-loss 시계열이 없었던 것이 아니라 **increase=0**이었다. 18:15 raw 값은 slot d/content/phase=collect 누적 3이다. collect label은 여러 경로가 공유하므로 이 3건이 renew 정체였다고 주장하지 않는다. ACTIVE+RETIRED 9건의 순간 표본도 아직 callback이 진행 중일 수 있다. 구조는 재현됐지만 해당 구간의 renew 정체 발생 횟수는 미확정이다.

## 별도 중요 결함: 일반 영상/최초공개 NEW_VIDEO 적격성

- 일반 영상은 `earliest_complete_effective_at`이 있어야 새 알림 후보를 만든다. Shorts는 초기화된 SHORT watermark를 사용한다.
- collector는 maxResults 10을 사용하고 중앙 실제 MAX_PAGES=1이다. helper는 **10번째 항목 직후 max_results로 종료**하므로 PARTIAL이다. 0~9개이고 continuation이 없으면 COMPLETE가 가능하다.
- 운영 활성 video_list 21채널 중 **11채널의 complete anchor가 NULL**이다. 그 11채널의 최근 24시간 내 마지막 수락 목록을 provider/kind/subject 인덱스로 조회했으며 **모두 PARTIAL/GAP_UNRESOLVED/10개**였다.
- 실제 reducer 프로브: anchor 없는 PARTIAL 일반 영상은 새 canonical 후보 1개·알림 후보 0개, anchor 있는 대조군과 initialized Shorts는 각각 알림 후보 1개.
- 따라서 10개 PARTIAL만 계속 받는 NULL-anchor 채널에서는 일반 영상·최초공개 NEW_VIDEO 후보가 생성되지 않는다. 향후 실제 COMPLETE가 생기는 경우까지 영원히 불가능하다고 일반화하지 않는다.
- 독립 reviewer도 다른 head 초기화 writer가 없고 수집 설정과 적격 조건이 맞지 않음을 확인했다. 과거 shorts 수정은 일반 영상 정책을 의도적으로 변경하지 않았지만, 무기한 억제를 허용한다는 제품 결정은 찾지 못했다.
- 실제 신규 업로드 누락 건수는 미확정이다. 오래된 영상이 재노출된 경우와 구분해야 한다. Shorts 방식으로 단순 전환하면 과거 영상 오발송 위험이 있으므로 **별도 정책/구현 범위**로 다룬다.

## 쇼츠 회복 범위와 발송 구간

- superseded publish는 관측·queue·checkpoint·lease terminal을 같은 tx에서 롤백한다. 폐기된 응답의 영상 목록은 보존되지 않아 과거 개별 쇼츠 포함 여부를 복원할 수 없다.
- cursor를 이어 읽는 방식이 아니다. 다음 수락 시점에도 공개 상태이고 관측 창 안에 남아 있으면 회복할 수 있다. 창 밖 이동·삭제/비공개는 영구 누락 가능성이지 확인된 누락 건수가 아니다.
- 신규 채널의 최초 기준 목록은 알림 없이 저장될 수 있다. 현재 활성 SHORT watermark는 **21/21 initialized**, missing 0이라 현재 기존 채널에는 이 미초기화 노출이 없다.
- 최근 24시간 shorts/video/community DEAD_LETTER는 0.
- 최근 수락 shorts 관측 200개: received→processed p50 1.220초, p95 2.012초, 최대 2.435초.
- 최근 outbox 1000행 중 24시간 NEW_SHORT 4건: delivery 4개 모두 SENT, created→sent p50 1.598초, p95 1.843초, 최대 1.844초.
- 두 표본의 분위수를 합산하지 않는다. 업로드→수집 수락 지연은 포함하지 않는다. 무조건적인 종단 exactly-once 보장도 주장하지 않는다.

## 경보: 원인 감시 누락이지 전체 경보 부재는 아님

- 경로: `prometheus/alerting/alerts.yml` → `grafana/build-alert-rules.py` → provisioning → Grafana 평가 → **외부 Alertmanager :9093** → ntfy-bridge. Prometheus rules API와 Grafana built-in AM이 비어 있어도 경보 부재로 판정하면 안 된다.
- TargetsStale 및 LiveUnresolvedUnreviewed가 외부 AM에서 active, inhibited/muted/silenced 모두 없음, receiver ntfy-bridge였음. 최근 6시간 Grafana sender 오류/drop 및 AM webhook 실패 집계 0. 특정 경보의 휴대전화 도달은 미검증.
- `youtubejs_channel_live_check`, `youtubejs_video_live`가 CallBudget뿐 아니라 TargetsStale/fleet freshness 범위에서도 빠져 있다.
- AttemptErrorRatioHigh와 PublishFailure가 `superseded`를 제외한다. renew fence-loss는 `phase=collect`로 기록되며 acquire 시 superseded는 metric/log 없이 종료된다.
- 일반 Grafana 변환은 모든 숫자 결과의 존재를 발화로 보고 `noDataState: OK`를 쓴다. **PromQL에 bool만 추가하면 0도 경보가 된다.** 규칙별 변환과 지표 부재 감시를 함께 설계해야 한다.
- `group_by=[alertname,job]`, repeat 12시간, alert-log는 kind를 버린다. 이것만으로 통지 실패를 확정하지 않는다.

## 도입 변경·인수한 retention 상태

- 보존 API image `reliability-f45d97a49-arm64`의 label은 `f45d97a491c1943727f8d7cd20aa816865a132f8`이고 부모는 `5a0ff59cd0dad69ee31e05f895c0948147a2c415`다. 두 revision의 당시 stale SQL이 같았다.
- migration 245/246/254는 9월 30일 21:28:20 KST 적용, Loki API 시작은 21:34:34 KST/version7.0.1. 배포 기록 `docs/current/plans/2026-09-30-alarm-worker-collector-reliability.md:274–276`도 f45 source의 중앙/AP 반영을 명시한다. 당시 container ID와 image ID를 직접 묶는 원시 영수증은 미확보.
- 관련 retention 수정은 조사 시작 때 미커밋 6개 파일이었지만 다른 세션에서 **API 7.2.3/43a5c57a1로 배포 완료**됐다. 최신 기록은 db-growth worktree의 `docs/current/plans/2026-10-03-projection-retention-capacity.md`와 6d15ada9 commit이다.
- 독립 검토: retention에서 차단 결함 없음. 64개 독립 배치, 기존 DB 전체 시한, partial commit 계수 보존, CURRENT/lease 보호, 7일 TTL을 유지한다.
- 이 변경은 만료 후 정리 처리량만 보완한다. 전체 snapshot 쓰기와 7일 보존 중 증가, 수집 결과 폐기는 해결하지 않는다. 동시 배포 기록상 cutoff를 넘긴 RETIRED가 0이라 운영 bulk drain 성능 실측도 아직 아니다. 이를 churn 해결로 보고하지 않는다.
- 원 조사 세션 식별: Codex `분석해 쇼츠 알람 지연 원인`, UUID `01a1010c-15f9-7be2-b658-29fb9848dc1b`. 보고서·관련 작업 상태를 인수했으나 직접 steer/강제 종료는 하지 않았다. 세션 wrapper는 Node24.20을 요구하고 호스트는24.21, 현재 OMP에는 CODEX_THREAD_ID가 없어 제어를 우회하거나 다른 세션을 사칭하지 않았다.

## 남은 작업과 안전 조건

- [ ] 다음 사용자 요청에 맞춰 구현·계약 변경 범위를 확정하고 최신 worktree/runtime을 확인한다. 조사 수치를 단순 확인하려고 전부 다시 실행하지 않는다.
- [ ] **P1:** 전체 discovery generation과 해당 작업의 유효성을 분리한다. snapshot/admission, renew, complete, 최종 publish를 함께 다룬다.
- [ ] **P1:** UNKNOWN의 최근 확인 여부를 대상 존재 여부와 분리하고 다음 확인 시각·예산으로 제어한다. UNKNOWN→ENDED 추정, 대량 수동 정산, 임의 backoff 상수로 숨기지 않는다.
- [ ] **P1 별도:** 일반 영상/최초공개의 complete anchor 문제를 해결한다. 기존 영상 재노출의 오발송 방지와 초기 기준 정책을 먼저 정한다.
- [ ] **P2:** projection retirement와 실제 owner 손실을 구분해 필요 시 fenced release한다. release만 빠르게 바꾸면 호출 낭비가 더 커질 수 있어 단독 근본 해결로 취급하지 않는다.
- [ ] **P2:** 6종 경보 범위, superseded 폐기, 실제 수락 간격, metric 부재, 전달 경로를 보완한다. 설정 호출 상한을 무작정 늘리지 않는다.

세대 분리의 필수 검증:
1. 무관한 영상 대상 변경 중 기존 content bundle의 수집·갱신·발행·checkpoint가 유지된다.
2. 실제 대상 해제/삭제·cadence/계약 변경은 기존 안전 계약에 맞게 차단된다.
3. 해제→재등록(ABA) 뒤 과거 lease를 허용하지 않는다. 현재 존재 여부나 재사용 가능한 hash만 비교하지 않는다.
4. owner/fence_epoch/scheduled_for/ACTIVE/만료와 관측 schema/contract generation 검사를 보존한다. 유효 CURRENT projection 자체의 존재도 계속 필요하다.
5. RETIRED target의 valid_until이 아직 남아 있다는 이유로 old target을 허용하지 않는다. publish의 확인과 저장은 같은 tx/잠금 경계로 보장한다.
6. retention이 lease를 삭제·재생성하면 epoch가 재시작할 수 있음을 고려한다. 작업별 revision은 재사용되지 않아야 하며 history 삭제에 종속되면 안 된다.
7. 전역 snapshot 쓰기 증폭도 줄었는지 확인한다. per-job fence만 고쳐도 UNKNOWN membership 자체가 흔들리면 DB 증가는 남는다.

영향은 API projection·DB lock/schema·collector/joblease·source publish·공유 proof 계약과 AP fleet에 걸칠 수 있다. 한 SQL의 CURRENT 조건 삭제로 축소하지 않는다. 새 계약/migration이 필요하면 승인과 coordinated cutover·rollback 범위를 명시한다. runtime host에서 build/test 금지, kapu에서 검증하고 승인된 서비스만 no-build/no-deps 반영한다.

구현 후에는 실제 PG의 기존 실패 시나리오와 위 안전 조건을 회귀로 확인하고 실제 경로 smoke를 실행한다. 최종 소스가 모인 뒤 해당 패키지/race/NilAway와 저장소 정본 local CI를 수행한다. 예시 정본 실행은 `RACE_TEST_PARALLEL=2 bash scripts/ci/local-ci.sh`; 새 우회/검증 생략/문서 문자열 gate를 추가하지 않는다. 운영 DB 조회는 매 세션 read-only=on 증명·statement_timeout 5초·제한 집계만 사용한다.

## 근거 파일 — 다른 세션에서도 절대 경로로 읽을 것

근거 디렉터리:

```text
/home/kapu/.omp/agent/sessions/-work-iris-stack-hololive-bot/2026-10-03T03-45-33-350Z_01a0ffdd-fca6-75cf-906c-0383a2c9fa66/
```

우선 `local/youtube-generation-investigation-final.json`을 읽는다. 후속 정정을 반영한 통합 결과다.
- `local/youtube-generation-boundary-investigation.log`: 실제 PG lease/snapshot/publish 경계 프로브.
- `local/youtube-content-eligibility-investigation.log`: 실제 reducer 4개 사례.
- `local/youtube-rpc-capacity-investigation.json`: 단계별 대기·처리, 상한, raw/increase counter 정정.
- `local/youtube-downstream-latency-investigation.json`: 수락 후 처리/발송 표본과 폐기 job 범위.
- `local/youtube-video-gate-exposure.txt`: NULL-anchor 11채널의 실제 수락 목록 형태.
- `local/youtube-collection-external-alertmanager.json`, `local/youtube-investigation-current-state.json`: 경보 경로와 runtime/config 표본.
- `CollectorAdmission.json`, `ObservationDeliveryGaps.json`, `CollectionAlertCoverage.json`, `ProjectionSafetyReview.json`: 초기 독립 조사 보고서. **후속 정정은 본 문서와 final.json을 우선**한다. 특히 16배 RPC 추정, 시계열 부재 해석, retention 미배포 설명은 폐기됐다.

`local://`와 `agent://`는 세션에 묶이므로 새 세션에서 같은 URI를 그대로 쓰지 않는다. 임시 프로브 소스는 이미 삭제됐다. 필요한 회귀는 현행 코드/기존 fixture에 맞춰 작성하되 사용자·운영에서 확인된 실패를 단순 재확인하기 위해 재실행하지 않는다.

## 후속 세션 재개 — 작업 공간·승인 경계 확인

사용자는 이 핸드오프를 읽고 subagent-driven 방식으로 상세히 후속 작업을 이어가되, 현재 작업 공간과 승인 범위를 먼저 확인하고 다른 세션 변경·기배포 수정을 보존하도록 요청했다. 읽기 전용 조사 3개와 독립 검토 2개로 범위를 나누었다. 새 schema·fencing 계약·알림 정책·운영 반영의 구체적 승인 범위는 기존 배포 승인과 별도로 결정한다.

### 실제 확인한 작업 공간과 운영 상태

- 기본 checkout: `refactor/api-ownership-20261003`, HEAD `a7316af71f6efa829cc1a2e677a53baf724739bb`. 시작 시 staged 14·unstaged 417·untracked 26개였다. 각 수치는 서로 겹칠 수 있는 Git 상태 항목이며 고유 파일 합계가 아니다. 해당 checkout은 편집하지 않았다.
- ssot checkout: HEAD `1942a78afaba45bb830bb63cbff33cdf5ae29e26`; 시작 시 이 핸드오프만 untracked였다. db-growth checkout: HEAD `6d15ada9ba9aad7e3b312613fb08c13abec6b45e`, clean.
- worker 수정 `1942a78af`와 API retention 수정 `43a5c57a1`의 공통 조상은 `5a3f03776`이다. 둘은 별도 갈래이므로 후속 구현 기준에는 양쪽 변경을 함께 보존해야 한다. 실제 통합·커밋은 아직 하지 않았다.
- collector-review checkout `9c9ed2678`은 clean이지만 운영 기준 `5a3`과 431개 경로가 다르다. 이를 이번 수정의 운영 소스로 간주하지 않았다.
- observability checkout `cefbfef4983800d66b21d8c75087116966090ead`에는 다른 변경 8개가 있다. README·compose·Jaeger·remote agents·metrics 수집·watchdog 테스트를 보존한다. `iris-client-go`에도 다른 변경이 있고 ssot의 local go.work가 이를 참조하므로, 향후 검증은 공유 모듈 입력까지 명시해야 한다.
- 기본 checkout의 migration `258_youtube_schedule_item_observation_clock.sql`을 확인했다. ssot/db-growth의 마지막 번호가 257이라는 이유로 258을 새로 배정하거나 기존 파일을 덮어쓰지 않는다.
- 중앙 API `43a5c57a1`, worker `1942a78af`, collector c `5a3f03776`, Seoul collector b `5a3f03776`: Docker metadata에서 모두 healthy·재시작 0을 확인했다.
- Osaka a/d: systemd active/running·재시작 0, current release 경로에 `5a3f0377659a`, 실제 H3 `/ready` 성공을 확인했다. a/d 모두 `queue_full=true`, d는 `discovery_truncated=true`였다. readiness 성공은 수집 지연 해소의 증거가 아니다. native 바이너리 전체 revision은 이번 조회에서 별도로 추출하지 않았다.
- 최초 중앙 inspect는 Compose 서비스명 `youtube-collector`를 컨테이너명으로 사용해 해당 항목만 실패했다. `docker ps`에서 확인한 실제 이름 `hololive-youtube-collector-c`로 수정해 성공했다. API·worker의 성공 결과는 재실행하지 않았다.
- kapu의 Go는 `go1.27.1 linux/amd64`, Docker server는 `29.8.2`다. 아직 제품 변경·build/test/lint·migration·배포·운영 쓰기·Git 쓰기를 하지 않았다.

### 새 설계 전제만 제한 조회한 결과

`hololive-osaka`의 `holo-postgres/hololive`를 socket 경로로 조회했다. 먼저 `SHOW transaction_read_only`가 `on`임을 확인했고, 각 실제 조회도 같은 guard와 `statement_timeout=5000`, `lock_timeout=1000`을 사용했다. 출력은 집계뿐이며 식별자·제목·payload를 내보내지 않았다. 기존 18:00~18:15 장애 수치는 재측정하지 않았다.

1. operational roster의 LIVE 및 지난 일정/시각 없는 legacy UPCOMING을 freshness·review receipt로 제외하기 **전**에 최대 1,001행으로 제한 집계했다. LIVE 21·UPCOMING 96, 합계 117이었다. 구조적 membership 후보의 현재 상한이 1,000 아래라는 근거일 뿐이며, 향후 상한 초과·공정성 정책을 생략할 근거는 아니다.
2. CURRENT enabled video_list target을 최대 128개로 제한하고, 각 채널의 최신 checkpoint가 가리키는 youtubejs 관측 하나만 payload dictionary와 연결했다. target 21·관측 21·영상 항목 181개에서 `published_at` 비NULL 0, `scheduled_for` 비NULL 1, `is_premiere=true` 1이었다. 현재 표본에서는 발행 시각으로 오래된 재노출과 신규 업로드를 구분할 수 없다. 전체 이력·향후 데이터의 시각 품질로 일반화하지 않는다.

이번 조회 원본 집계와 runtime metadata:

```text
/home/kapu/.omp/agent/sessions/-work-iris-stack-hololive-bot/2026-10-03T10-50-50-425Z_01a10163-58b9-7195-bc17-340aae110bb5/local/collection-churn-followup-evidence.json
```

### 독립 검토 결과와 후속 구현의 필수 경계

조사 3개·독립 reviewer 2개가 모두 종료했다. 아래는 부모가 채택한 제약과 미확정 사항이며, 새 코드가 동작한다는 검증 결과가 아니다. 각 보고서의 원안보다 아래의 정정과 실제 조회를 우선한다.

#### P1 세대 분리·대상 안정화

- **기각:** 현재 존재 여부나 동일 hash만 비교하기, 종류별 revision만으로 모든 작업을 보호한다고 주장하기. 후자는 `EXACT_SUBJECT` video_live 작업에서 무관한 다른 영상의 변경도 전파한다.
- **구현 후보:** `(subject, observation_kind)`별 연속 유효성을 CURRENT target에 보존한다. 기존 generation identity를 `member_since_generation`처럼 사용하고, 의미가 바뀌거나 재등록되면 새 generation을 부여하는 방식은 별도 sequence보다 단순할 수 있다. lease에는 acquire 당시의 job scope와 필수 bundle count를 기록해 CURRENT row와 비교한다. 삭제 감지를 위해 RETIRED history를 다시 읽는 방식은 채택하지 않는다. 실제 cadence/enable/contract 변경, ABA, owner/epoch/scheduled_for/ACTIVE/expiry 검사는 계속 보존해야 한다. priority 변경을 유효성에 포함할지는 현행 scheduling 계약을 대조해 구현 전에 명시한다.
- **잠금:** CURRENT row를 기다리다가 RETIRED로 바뀌어 0행을 얻는 snapshot 경합이 있다. 임의로 두 번 재시도하는 함수는 해결 증거가 아니므로 기각했다. 고정 head row의 share/update 잠금과 잠금 이후 별도 statement snapshot을 쓰는 후보를 실제 PG에서 검증해야 한다. acquire/publish/refresh의 lock order와 기존 nonblocking acquire 계약도 함께 검증한다. 이 후보는 아직 실행 증명이 없다.
- **UNKNOWN과 LIVE:** freshness는 membership을 바꾸지 않고 다음 확인 시각을 제어하도록 분리한다. UPCOMING뿐 아니라 LIVE도 positive freshness에 따라 출입하므로 LIVE를 그대로 두면 전체 snapshot 쓰기 증폭이 남는다. 기존 270초 정책에서 도출한 `not_before`를 신규 후보 선정·acquire 전용 검사에 적용하고, 이미 획득한 작업의 renew/snapshot/complete/publish에는 적용하지 않는다. 특히 `0144_04`는 acquire뿐 아니라 CompleteCurrent도 공유하므로 여기에 due 필터를 넣어서는 안 된다.
- **상한:** 현재 구조적 후보 상한 117개는 여유가 있지만, 고정 정렬로 1,000개만 남기고 나머지를 영구 굶기는 정책은 승인하지 않았다. 초과 시 누락·공정성·실패 표현을 명시해야 한다.
- **retirement 구분:** owner 손실과 대상 유효성 상실을 구분한 fenced release는 per-job fencing과 함께 검증한다. release만 앞당기는 별도 변경으로 호출 낭비를 키우지 않는다.
- **heartbeat:** validity 연장을 절반 시점으로 미루는 제안은 API 장애 후 last-good grace를 약 1시간에서 최소 약 30분으로 줄일 수 있다. 이를 계약 불변의 무료 최적화로 취급하지 않는다. last-good grace를 보존하는 쓰기 감소 방식과 함께 검토한다.
- **승인 필요 근거:** `docs/current/architecture/youtube-three-provider-convergence-contract-v2-20260814.md:677–684`, 특히 683행은 이전 generation fetch의 publish 거부를 명시한다. per-job validity는 이 계약과 DB schema/API·collector 구현을 함께 바꾸는 작업이며, 현행 계약을 몰래 완화하는 SQL 패치로 처리할 수 없다.

#### P1 일반 영상·최초공개 정책

- 현행 payload에는 순서가 보존되지 않고, 이번 181항목 표본에 published_at도 없었다. 현재 목록만으로 “저장된 적 없는 오래된 영상의 재노출”과 “새 업로드”를 구분할 수 없다. PARTIAL을 COMPLETE로 위장하거나 일반 VIDEO watermark의 initialized 값만 신뢰하지 않는다.
- 별도 baseline 열을 반드시 추가해야 한다는 최초 제안도 아직 확정할 수 없다. 기존 known-set/clock을 활용하는 대안은 있으나, 그 의미가 실제 최초 관측 기준인지 확인해야 한다.
- 추가 guarded 조회: NULL complete-anchor 채널 11개 모두에 non-short clock이 있고 최소 10개씩 있었다. 그러나 **11채널 모두** `first_positive_effective_at <= 0001-01-01`인 clock이 존재했고, 활성 video_list 채널 전체에서 해당 행은 161개였다. 따라서 기존 clock의 최솟값을 실제 baseline 시각으로 사용할 수 없다. 레거시 row 재관측 경로가 원인이라는 설명은 코드상 후보이며 실제 쓰기 추적·회귀로 확정하지 않았다. 현재 gate에서는 알려진 영상이 새 알림 후보가 아니므로 이 값 자체를 새 운영 알림 결함으로 선언하지 않는다.
- clock/known-set 기반 즉시 활성화는 “기존 저장 영상이 기준임을 수용한다”는 정책 선택이다. 과거 미저장 영상의 오발송 가능성을 제거하지 않는다. 별도 새 baseline은 첫 목록을 무알림으로 처리하는 누락 trade-off가 있다.
- video_list의 claim 선행 순서를 추가해도 나중에 삽입된 관측·DLQ/replay까지 임의 도착 순서 수렴을 보장하지 않는다. 처리 순서 완화책과 수렴 증명을 혼동하지 않는다.
- 미래 scheduled_for가 있는 최초공개만 예외로 처리하는 후보는 일반 영상 문제 전체의 해결이 아니다. 기준 목록의 최초공개, 공개 전환 후 중복, 이미 알려진 canonical 영상·short 교차 분류를 별도로 검증해야 한다.
- 정책 결정은 (a) 오발송 방지를 유지하면서 신뢰 가능한 신규성 근거 수집까지 계약을 확장하거나, (b) 기존 저장 목록 이후 처음 보는 ID를 알리되 과거 미저장 영상의 재노출 알림 위험을 명시적으로 수용하거나, (c) 이 별도 P1을 보류하는 선택이다. 임의로 (b)를 채택하지 않았다.

추가 clock 조회 근거는 앞의 세션 디렉터리 아래 `local/collection-churn-baseline-clock-evidence.json`이다.

#### P2 경보·관측

- `bool` 0도 현행 Grafana 변환에서는 숫자 존재로 발화하므로 filtered PromQL을 유지한다. 누락은 별도 absence 조건으로 표현한다. 새 converter를 도입할 이유는 확인하지 못했다.
- channel_live_check는 동일 cadence의 기존 경보 범위를 확장할 수 있다. video_live는 현행 stale-only membership 때문에 단순 stale 비율 적용 시 구조적 오경보 위험이 있다. due age·현재 작업의 지속 여부·오류 및 폐기를 함께 보아야 한다. 24시간 중 한 번이라도 대상이 있었다는 조건만으로 absence 경보를 유지하면 이미 제거된 작업에도 발화한다.
- `superseded > success`를 전체 시도의 50% 폐기라고 부르는 제안은 기각했다. timeout/failed 등 다른 결과가 빠지기 때문이다. 기존 `youtube_observation_publish_total`의 observation kind별 superseded/실제 전체 publish 결과 비율을 사용하고, job-kind로 기록되는 empty는 분리하는 후보가 더 직접적이다. publish 이전 snapshot/acquire/renew 실패는 이 비율로 포착하지 못하므로 하한 신호임을 명시해야 한다.
- checkpoint만으로 실제 수락 간격을 판단하는 제안도 기각했다. tab이 없는 정상 empty COMPLETE는 checkpoint 없이 lease 완료만 기록할 수 있다. 실제 수락 관측은 observation acceptance와 정상 empty completion을 구분하는 계약·회귀가 필요하다. 현행 lease staleness와 중복되는 새 지표를 무조건 추가하지 않는다.
- 여섯 종류 전체의 호출 예산을 보려면 persistent membership·not_before에 맞는 API demand/due 의미도 함께 바꿔야 한다. 임시로 video_live를 영구 제외한 5종 규칙을 P2 완료라고 보고하지 않는다. 기존 146–152% 수치는 당시 명목 수요이며 실제 호출량·재시도 배율로 바꾸어 해석하지 않는다.
- acquire-time superseded는 관측되지 않으며, renew fence-loss의 phase와 publish 실패의 collect 중복 계측 후보도 있다. renew·publish·callback 경로를 구분한 focused 회귀 후 수정해야 한다. 과거 collect 누적 3건의 원인을 이 정적 발견으로 소급 확정하지 않는다.
- 기존 외부 Alertmanager→ntfy 경로는 보존한다. alert-log가 kind를 버리는 문제와 실제 휴대전화 도달은 별도다. 후자는 여전히 미검증이다.
- **중요 활성화 경계:** `/home/kapu/work/observability-stack/alert-log.sh`는 systemd가 checkout에서 직접 실행한다. Grafana provisioning도 live bind mount다. 단순 로컬 파일 편집이 다음 timer나 다른 세션의 Grafana 재시작 때 활성화될 수 있으므로, 수정은 별도 비활성 worktree에서 준비하고 live tree 전송·reload는 승인 후에만 한다.

### 승인 뒤 실행할 검증과 전환 범위

- 소스 기준은 worker `1942a78af`와 API retention `43a5c57a1`을 모두 보존한 격리 checkout이다. 다른 세션의 migration 258과 소스 변경을 무단 인수·재번호화하지 않는다.
- 실제 PG 검증: 무관한 다른 종류 및 같은 종류 다른 subject 변경 중 수집·renew·publish·checkpoint·complete 유지; 자신의 제거/disable/cadence/contract 변경 거부; ABA; lease delete/recreate; owner/epoch/slot/expiry; 유효 CURRENT 부재; publish와 refresh 경합; not_before 갱신이 admitted 작업을 취소하지 않음; CURRENT/lease retention 보호; freshness 갱신 중 generation·전체 target 쓰기 안정화; 상한 초과 정책.
- 알림 정책 검증: PARTIAL/empty COMPLETE/empty PARTIAL, known/unknown/legacy-year1 clock, 오래된 재노출, 지연·동일시각·재생 관측, 첫 기준의 최초공개, UPCOMING→공개 중복, short 교차 분류. 선택한 정책에서 보장하지 않는 경우는 명시한다.
- 관측 검증: real PromQL evaluator와 Grafana converter/격리 평가에서 0·빈 roster·지표 부재·대상 제거·snapshot 실패·정상 empty completion·폐기 분모를 실행한다. 실제 경보 규칙/지표의 동작 회귀만 두고 checker 자체 테스트·문서 문자열 gate를 추가하지 않는다.
- 통합 후 영향 패키지/race/NilAway·정본 local CI와 필요한 stack DB/retry/projection 검사를 수행한다. 아직 새 구현이 없으므로 이번 재개에서는 이 검사들을 실행하지 않았다.
- 운영 전환은 별도 승인이다. 선택지는 bounded collector drain 후 API·fleet 동시 cutover 또는 검증된 중간 API artifact를 이용한 단계적 전환이다. 중간 artifact 없는 현재 상태에서 단계적 배포가 가능하다고 단정하지 않는다. worker는 교체하지 않고 retention을 보존하며, API·DB·collector a/b/c/d·관측 설정 각각의 복구점과 중단 영향을 승인 전에 제시한다. Git publication·새 의존성·호출 상한 증가는 포함하지 않는다.

### 보존한 독립 보고서

앞의 재개 세션 디렉터리 아래에 다음 파일을 저장했다. 원안의 기각된 제안과 이후 reviewer 정정은 이 문서가 우선한다.

- `local/collection-churn-ChurnContractMap.txt`
- `local/collection-churn-VideoBaselinePolicy.txt`
- `local/collection-churn-CollectionMonitoringMap.txt`
- `local/collection-churn-FencePolicyReview.txt` — claim-ordering finding의 잘못된 placeholder locator는 실제 `hololive-api/internal/youtube/sourceobservation/queries/repository_claim_0012_12.sql:103–112`로 정정한다. clock baseline의 신뢰성 주장은 위 실제 조회에 따라 철회됐다.
- `local/collection-churn-AlertSemanticsReview.txt`

이번 재개에서 변경한 저장소 파일은 이 핸드오프뿐이다. 실제 수행 증거는 runtime metadata·a/d H3 readiness·read-only 집계이며, 제품 수정·회귀 성공·운영 개선을 주장하지 않는다. Fallback delta: none.

### 후속 사용자 승인

사용자는 위 검토 뒤 다음을 명시적으로 선택했다.

- **P1·P2 로컬 구현 승인:** worker·API retention을 보존한 격리 worktree에서 migration 파일·API·collector·계약·관측 설정을 함께 준비하고 실제 PG smoke·race·NilAway·정본 local CI를 수행한다.
- **일반 영상 정책: 신규성 근거 확보 우선.** 기존 호출 상한 안에서 신뢰 가능한 신규성 근거 수집·계약 변경까지 다루며, 근거가 부족한 항목은 억제한다. 과거 미저장 영상의 오발송을 수용하는 known-set 즉시 활성화는 선택하지 않았다.
- 운영 DB 적용·배포·live 관측 설정 반영·Git 게시·새 의존성·호출 상한 증가는 승인 범위가 아니다.

### 승인된 로컬 구현 및 실제 경로 검증

- 구현 worktree: `/home/kapu/work/iris-stack/hololive-churn-20261003`, branch `fix/collection-churn-20261003`. worker `1942a78afaba45bb830bb63cbff33cdf5ae29e26`에서 분기하고 retention `43a5c57a1ad7a3d4aed95f95bc981c9a65688953`을 `cherry-pick --no-commit`으로 보존했다. 새 commit·게시·운영 적용은 하지 않았다.
- 관측 준비 worktree: `/home/kapu/work/observability-churn-20261003`, branch `fix/collection-churn-20261003`, 기준 `cefbfef4983800d66b21d8c75087116966090ead`. live observability checkout·systemd timer·Grafana bind mount는 변경하지 않았다.
- migration은 259 membership/guard/eligibility, 260 video publication baseline/pending이다. 다른 세션의 258은 인수하거나 재번호화하지 않았다. 현재 API·worker의 배포 변경과 기본 checkout의 다른 작업은 통합 대상으로 삼지 않았다.
- projection은 구조적 LIVE/검토 대상 UPCOMING membership과 `not_before`를 분리한다. 같은 hash refresh의 유효 기간·eligibility UPDATE는 남지만 freshness 변화마다 전체 generation/target snapshot을 INSERT하지 않는다. 자신의 cadence/priority/enabled 변경·제거/재추가는 새 membership이며, 무관한 변경은 논리 생성 시각과 연속성을 보존한다. 1,000개 상한 초과는 부분 projection을 만들지 않고 오류로 남긴다.
- collector는 취득 scope의 kind·subject 방식·개수와 CURRENT target의 `member_since_generation`을 검증한다. owner/epoch/slot/expiry fence와 observation/checkpoint/queue/terminal 원자성은 유지한다. acquire/renew/publish superseded를 구분하고 cancel→join→fenced release를 지킨다. join timeout은 release 성공으로 바꾸지 않는다.
- 일반 영상은 목록 두 RPC 뒤 별도 limiter를 통과하는 player RPC 최대 두 번으로 publication을 확인한다. hidden per-row player 호출·상대 날짜 추정을 제거했고 fleet 호출 상한은 늘리지 않았다. generation 2 cursor는 최근 증거·회전 진척을 durable checkpoint에 저장하며 긴 목록과 새 head를 처리한다. 손상 cursor는 fail-closed이며 generation 1을 새 cache로 재해석하지 않는다.
- 첫 비어 있지 않은 PARTIAL/검증된 complete-empty 목록은 조용한 기준이다. 빈 PARTIAL·year-1 clock은 기준 증거가 아니다. 과거 재등장·이미 알려진 ID·기준 최초공개는 억제하고, 기준 이후 게시가 확인된 pending 영상과 새 미래 최초공개만 한 번 알린다. 부분 목록을 COMPLETE 부재 근거로 승격하지 않는다. 첫 기준 이전과 목록 범위 밖·증거 부족 항목의 누락 복구, 과거 자동 backfill은 보장하지 않는다.
- 영상/Shorts claim은 채널·kind 선두만 선택한다. 활성 backlog 순회/정렬 비용과 실제 잠금 후보 조회 비용을 분리한다. 5만 활성 행 fixture의 custom/generic plan 및 실제 선두 4개 claim이 통과했다. 독립 검토에서 materialized CTE 재조회 계수 누락을 지적해 계수와 1초 SQL 시한을 추가했으며 최종 검증 대상에 포함한다. 임의 도착·replay의 완전 수렴이나 모든 동시 경쟁 부하를 입증했다고 하지 않는다.

실제 실행:

1. disposable PostgreSQL 18.6에서 독립 `go run -race` 경로를 실행했다. freshness refresh 50회 동안 generation 1개·target 3개를 유지했다. 무관한 CURRENT 교체 뒤 기존 proof의 snapshot/renew/publish, observation/checkpoint/queue 각 1행과 API canonical Shorts 1행을 확인했다. admitted 이후 `not_before` 변경은 진행을 취소하지 않았고 자기 변경·ABA·이전 owner는 거부했다.
2. 실제 ContentRunner→publisher→API consumer를 PG에 연결한 신규성 smoke에서 NEW_VIDEO outbox 누계가 `0, 0, 1, 1, 2, 2`였다: 최초 부분 기준, 과거 재등장+근거 미확정, 후속 게시 근거, cache 재관측, 새 최초공개, 공개 전환 순서다. 각 player 호출은 `1, 2, 1, 0, 1, 1`회였고 COMPLETE anchor는 끝까지 NULL이었다. 외부 provider는 결정적 fixture이므로 이를 live provider 전체 경로 검증으로 부르지 않는다.
3. 별도 익명 실제 player 조회 3건에서 identity/channel 일치와 정확한 RFC3339 publication 근거 3건을 확인했다(PUBLIC 1, MEMBERS_ONLY 2). 표본 3건이며 전체 채널 커버리지 증거가 아니다.
4. helper `npm test` 363개와 typecheck가 통과했다. 영향 Go 패키지 검증에서 collector lease/publisher/helper, API source-observation/targetprojection, 공유 계약, schema golden 및 최소 권한 검사가 통과했다. 발견한 세대 fixture·deadline fixture·직접 lease scope·serial consumer tick·초과 길이 ID·다중 SQL prepared statement 오류를 수정했다. 최종 race/NilAway/local CI 결과는 아래 확정 기록을 따른다.
5. 비활성 관측 tree에서 promtool 규칙 검사와 fixture 5개, generator 검사/동기화, Compose config, shell syntax가 통과했다. 실제 격리 Prometheus 3.14.0/Grafana 13.2.2에서 여섯 규칙 발화·0/0 비발화·missing-kind 발화를 확인했다. smoke의 `for`는 0초로 줄였으므로 운영 10분 대기를 실측했다고 하지 않는다. 실제 alert-log는 두 kind를 구분하고 중복/해소를 처리했다. 휴대전화 수신은 미검증이다.

기본 checkout 보호 관련 사고: 한 test 보정 agent가 잘못된 상대 경로로 기본 checkout의 세 파일을 수정했다. agent 자신의 hunks만 되돌린 뒤 parent가 초기/복구 snapshot anchor `6B26`, `E6DB`, `F66B` 일치를 확인했다. 전체 사전 사본은 없으므로 그보다 넓은 무변경 증명을 주장하지 않는다. 이후 모든 편집 경로는 격리 tree 절대 경로로 제한했다. 다른 세션의 기존 fixture 변경은 보존했다.

근거는 같은 세션의 `local/collection-churn-application-smoke.json`, `local/collection-churn-novelty-smoke.json`, `local/collection-publication-probe-result.json`, `local/collection-churn-observability-verification.json`, `local/collection-churn-checkout-restoration.json`이다. 임시 Go smoke 세 개·driver·격리 runtime은 제거했다. 실제 운영 개선·휴대전화 전달·배포 완료를 주장하지 않는다.

#### 최종 통합 검사에서 추가로 고친 경계

- 정본 local CI의 architecture·workspace/tidy·canonical/workspace vet·integration-tag vet·staticcheck가 통과했다. 최초 lint 210건은 파일 소유권을 나누어 오류 처리·복잡도·현대 Go 문법·스타일 원인을 수정했고 gate나 linter를 완화하지 않았다.
- 그 뒤 parent가 근거 조회 시한 만료와 비강등 오류의 경합을 추가로 확인했다. 설정 오류가 시한 직후 반환되면 COMPLETE로 바뀌는 실패를 실제 회귀 시험에서 재현한 뒤, 시한 여부가 아니라 기존 오류 분류로 강등을 판단하도록 고쳤다. 부모 취소·소유권 상실과 설정/내부 오류는 계속 반환한다. 이 finding은 앞선 독립 정적 검토의 무결함 보고 이후 발견한 것이다.
- 격리 candidate를 `hololive-bot`으로 연결한 임시 meta view에서 정본 `check-stack-db-access-policy.sh`가 통과했다. 다른 저장소는 읽기만 했고 임시 view는 제거했다. Iris native read-model projection/응답 retry 상수는 변경하지 않았으므로 해당 전용 gate를 이 YouTube 변경의 검증으로 내세우지 않는다.
- 독립 검토 잔여 한계: 큰 목록 설정에서는 cache 순환으로 미확정 head 재조회가 늦어질 수 있다. 예정 최초공개의 cached 예정 시각은 공개 전환까지 재사용한다. 손상 cursor는 그 채널의 Shorts까지 막으며 clock skew가 미래 증거 거부를 일으킬 수 있다. 이를 성공·자동 복구로 숨기지 않는다. 검토 원문은 `local/collection-churn-FinalClaimReview.json`, `local/collection-churn-FinalNoveltyReview.json`에 있다.

- lint refactor 뒤 실제 runner→PG publisher→API smoke를 다시 실행했다. 알림 의도 누계 `0,0,1,1,2,2`, player 호출 `1,1,1,0,1,1`, COMPLETE anchor NULL을 확인했다. 조회 시한 뒤 비강등 오류 회귀와 정상 timeout 목록 보존 회귀 모두 통과했다. 5만 행 custom/generic claim의 재조회 계수·1초 SQL 시한도 통과했다.
- 구조적 LIVE/UPCOMING fixture의 `scheduled_for != effective_at` 오류를 기존 DB clock 계약에 맞게 수정한 뒤 해당 PG 시험이 통과했다. constraint를 완화하지 않았다. 마지막 lint 잔여는 긴 cursor 공정성 시험의 유지보수 지수였으며 durable cursor 직렬화/재읽기를 helper로 분리해 기존 assertion을 보존했다.
- 추가 근거: `local/collection-churn-final-path-verification.json`. 최종 smoke의 임시 source/driver/PG도 제거했다. smoke source가 남아 있던 병행 CI 한 번은 SQL ownership gate에서 정상 거절됐고, 제거 뒤 정본 명령을 재실행했다.

- 정본 lint 0건·NilAway·Go build·collector production JSON 시험/빌드·X Spaces helper 시험까지 통과했다. AP rsync manifest에서 새 Go/SQL 의존성 6개 누락을 발견해 명시 목록에 추가했으며 다음 실행에서 통과했다.
- 성능 gate가 `102,508,904 ns/op`, 36개 관측 모델 `3,690,320,544 ns > 3,600,000,000 ns`로 실패했다. 임시 PG auto_explain probe에서 publish 95–98ms, consume 6–14ms를 분리했고, UNIQUE 잠금 함수의 기본 SRF 추정 1,000행 때문에 단일 입력을 100,000행으로 부풀려 JIT 약 89–99ms를 매번 수행함을 확인했다.
- migration 259에 두 함수의 실제 cardinality `ROWS 1`을 지정했다. 잠금·fresh snapshot·충돌 판정·JIT 설정·성능 예산은 바꾸지 않았다. 수정 후 publish 5.38–5.66ms(첫 실행 18.73ms), 정본 benchmark `10,022,369 ns/op`, 모델 `360,805,284 ns`로 통과했다. 원인/수정 후 probe는 각각 artifact 360/362이며 임시 probe 파일은 제거했다. 최종 schema golden을 다시 생성하고 전체 CI를 계속한다.

- 전체 Go 시험에서 기존 dispatcher 통합 시험이 `NOAUTH HELLO`로 실패했다. 원인은 parent가 `INTEGRATION_TEST=true`를 전역으로 주어 정본 스크립트의 임시 Valkey 준비 전에 통합 시험을 활성화한 실행 오류였다. 공유 Valkey 인증을 읽거나 변경하지 않았다. 이후 명령은 전역 `INTEGRATION_TEST` 및 외부 test DB/Valkey 주소를 제거하고 `RUN_INTEGRATION_TESTS=true`만 사용해 마지막 정본 integration 단계가 격리 PostgreSQL/Valkey를 생성하도록 한다. 테스트를 삭제하거나 skip flag를 새로 추가하지 않았다.

### 최종 로컬 검증 완료

아래 정본 명령이 종료 코드 0, `[LOCAL CI] Passed`로 끝났다.

```bash
env -u INTEGRATION_TEST -u TEST_VALKEY_ADDR -u TEST_VALKEY_HOST \
  -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN \
  GOMAXPROCS=2 RACE_TEST_PARALLEL=2 RUN_INTEGRATION_TESTS=true \
  bash scripts/ci/local-ci.sh
```

- 실행 위치는 격리 `hololive-churn-20261003`이다. 전체 workspace 9개 package pattern을 검증했고 기존 sibling `iris-client-go`/`shared-go`도 현재 로컬 입력으로 포함했다. 다른 세션의 sibling 변경을 이 작업에서 수정·인수한 것은 아니다.
- architecture·SQL ownership·migration manifest·tidy·canonical/workspace/integration-tag vet·staticcheck·golangci-lint **0 issues**·NilAway **7 patterns**·Go/production build·AP rsync manifest·DB capacity·전체 Go 시험·`-race -p 2`가 통과했다.
- 별도 provision한 PostgreSQL/Valkey에서 integration-tag 2개 package와 INTEGRATION_TEST 4개 package가 통과했다. 정상 cleanup 단계까지 끝났다.
- 최종 성능 gate: **9,826,969 ns/op**, 36개 관측 모델 **353,770,884 ns**, 기존 예산 **3,600,000,000 ns**. 정본 기준이나 JIT를 완화하지 않았다.
- 전체 출력은 이 세션 `artifact://365`이며 영속 backing log는 세션 디렉터리의 `365.bash.log`이다. 로컬 실행 증거일 뿐 Git revision의 게시·release artifact 서명·운영 전환 증거가 아니다.

승인된 P1/P2 **로컬 구현·검증은 완료**다. 계약·서비스 문서·collector cutover/rollback runbook·CHANGELOG·AP 전송 목록을 함께 갱신했다. 운영 migration/배포·live 관측 설정 반영·휴대전화 수신 확인·Git commit/publication은 수행하지 않았다. 다음 운영 단계에는 별도 승인과 migration 259/260을 포함한 coordinated drain/cutover 및 복구점 확인이 필요하다. 구 artifact만의 자동 롤백은 새 DB 계약과 호환되지 않는다.
