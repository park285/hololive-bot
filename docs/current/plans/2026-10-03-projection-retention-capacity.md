# 수집 대상 이력 정리 처리량 보완

운영 revision `5a3f03776` 기준의 독립 worktree에서 구현한다. 기본 checkout의 다른 작업은 보존한다.

현재 상태(2026-10-03 19:25 KST): 중앙 API 7.2.3 / `43a5c57a1` 운영 반영과 두 정리 주기 확인을 완료했다. Git 원격 게시와 기본 checkout 통합은 수행하지 않았다.

## 확인한 문제와 범위

- 10월 3일 읽기 전용 운영 조회: 최근 24시간 8,573세대·대상 5,166,992행 생성. DB는 19.95 GB이고 최근 24시간 2.43 GB 증가했다.
- 최근 두 세대는 video_live_check 1개 추가 외에는 대상이 같았다. 신선도에 따른 대상 출입은 현행 수집 계약이다. 이번 변경에서 대상·poll 주기·generation hash·7일 보존·lease 보호를 바꾸지 않는다.
- 정리 함수는 트랜잭션당 reasons+targets 최대 1,000행을 지운다. runtime이 120초마다 한 번만 호출해 하루 최대 72만 행으로 유입보다 작다.
- 한 tick에서 최대 64개 독립 배치를 처리하되 기존 전체 DB 시간 제한을 공유한다. 진척 없음·오류·취소 시 즉시 종료한다. 실패한 문장을 재시도하지 않는다. 완료된 배치의 삭제 계수는 뒤 배치 오류에도 보존한다.
- 세대별 전체 snapshot의 쓰기 증폭과 이미 할당된 디스크 공간은 별도 한계로 보고한다. 신규 schema·TTL 축소·데이터 재작성·운영 활성화는 이번 로컬 변경에 포함하지 않는다.

## 남은 작업과 검증

- [x] 배치 누적·상한·중단·부분 성공 계측 회귀를 추가하고 수정 전 실패를 확인한다.
- [x] runtime의 bounded drain을 구현한다. source retention의 독립 실행을 유지한다.
- [x] 실제 PG에서 약 600개 target/reason을 가진 다수의 오래된 세대와 CURRENT·lease 보호를 검증하고 tick 처리량을 측정한다.
- [x] 영향 패키지 race·lint·빌드와 stack DB/retry/projection 검사를 실행한다.
- [x] 결과·한계와 운영 반영 필요 범위를 기록한다. 원격 게시·배포·운영 SQL은 승인 전 실행하지 않는다.

## 로컬 결과

- PostgreSQL 18.6 격리 DB: 만료 세대 16개에 각각 target/reason 600개를 적재하고 CURRENT·유효 ACTIVE lease 세대 2개를 별도로 둔다. 구 코드는 한 배치만 처리해 18세대가 남았고, 수정 후 19,200개 자식 행과 만료 세대 16개가 약 255 ms에 정리됐다. 보호 대상의 target/reason 600개씩은 모두 남았다. 운영 전체 DB·동시 부하 성능으로 외삽하지 않는다.
- 회귀 6개: 다중 배치와 동일 cutoff/시한, 64배치 상한, 오류 시 중단·부분 commit 계측, 취소, 전체 deadline 소진 뒤 source 정리 독립 실행, 실제 PG 처리량·보호. 초기 PG fixture의 activation 필드를 보정한 뒤 구 코드의 처리량 실패를 다시 확인했다.
- `GOWORK=off GOMAXPROCS=2 go test -race -p 1 ./internal/planes/youtube/runtime ./internal/planes/youtube/targetprojection -count=1 -timeout=8m` 통과(runtime 10.820초, targetprojection 5.259초).
- 영향 두 패키지 golangci-lint 0 issues, 저장소 pinned NilAway 통과, `GOWORK=off go build ./cmd/...` 통과. lint의 최초 신규 테스트 서식·사전 할당·float 비교 지적은 수정했다.
- 후보 worktree의 DB access policy 통과. 기본 stack의 DB/retry/projection 계약 검사도 통과했다. 뒤 세 검사는 기본 checkout에 대한 검사이며 후보의 변경 코드는 위 패키지 검사로 검증했다.
- 신규 migration·의존성·환경 변수 없음. 운영 반영 대상은 Hololive API 바이너리이며 collector/worker 프로토콜·DB schema·TTL은 불변이다. 롤백은 이전 API artifact로 가능하지만 이미 정상 만료로 삭제된 이력은 코드 롤백으로 복구되지 않는다.
- **당시 미실행:** 로컬 검증 시점에는 커밋·원격 게시·운영 배포·운영 데이터 변경을 하지 않았다. 후속 승인에 따른 운영 반영은 아래 실행 기록을 따른다. Git 게시 승인은 별도다.
- **남은 한계:** 전체 snapshot 재생성의 쓰기 증폭은 남는다. 7일 TTL 안의 이력 증가와 파일의 빈 공간은 이 수정으로 즉시 줄지 않는다. 운영 처리량·DB 정상 상태 용량과 기존 40%/30% 목표 달성은 미확정이다.

Fallback delta: none. 오류 무시·추가 재시도·retention 기간 축소는 없다.

## 운영 반영 재개

후속 사용자 진행 승인으로 중앙 Hololive API 7.2.3의 로컬 release commit·전체 local CI·ARM64 image 빌드·전송·API 단독 재생성·사후 검증을 수행한다. Git 원격 게시와 다른 runtime 교체는 포함하지 않는다. 현재 API revision은 기준 `5a3f03776`과 일치하며 worker는 별도 후속 revision으로 바뀌었으므로 기존 worker와 deploy tree는 보존하고 API image와 API VERSION만 전환한다. 새 migration이 없어 ledger는 조회·대조만 한다.

## 운영 실행 결과

- 소스: 로컬 release commit `43a5c57a1ad7a3d4aed95f95bc981c9a65688953`, API 7.2.3. clean worktree에서 `LOCAL_CI_GO_SCOPE=changed`와 기준 `5a3f03776`으로 정본 `scripts/ci/local-ci.sh` 전체 파이프라인을 실행했다. architecture·canonical vet·staticcheck·lint 0·NilAway·build·collector production/helper·성능 예산·API 모듈 전체 test/race 통과. 별도 opt-in integration lane은 미실행이며 실제 PG 검사는 기본 test/race에 포함됐다. 배포 정적 계약 5개도 통과했다.
- kapu `kapu-multiarch`에서 ARM64 image를 빌드했다. image ID `sha256:64561bd8ad96fcf70067e793c467309d9903343b0c36c8936c6822c56518c20a`, version 7.2.3 및 full revision 일치를 로컬과 원격에서 확인했다. 41,390,080-byte image archive와 API VERSION archive·후보 Compose override의 SHA256을 전송 후 대조했다. 원격 build는 없었다.
- 실제 운영 manifest·checksum 118개는 후보와 모두 일치했다(read-only guard `on`). migration을 실행하지 않았다. 운영 Compose 4개·wrapper·version helper·manifest도 후보와 hash가 같아 교체하지 않았다.
- candidate override의 `--check-config`가 통과했다. 첫 원격 명령은 Compose run이 stdin을 소비해 설정 검사 뒤 종료됐다. API가 이전 image/version이고 rollback 경로도 없는 것을 확인한 뒤 해당 probe의 stdin을 `/dev/null`로 분리해 실행했다. 이를 첫 시도의 배포 성공이나 자동 롤백으로 기록하지 않는다.
- `change_started_at=2026-10-03T10:23:31Z`; 새 API `StartedAt=2026-10-03T10:23:34.055252417Z`. 기존 wrapper의 `up -d --no-build --no-deps --pull never hololive-api`로 전환했고 약 6초 뒤 healthy/restarts 0이었다. 모듈 VERSION만 7.2.3으로 설치했으며 기존 `0664 root:root`를 유지했다.
- worker, central collector/issuer, PostgreSQL, Valkey는 사전·사후 container ID와 restart count가 모두 같았다. issuer의 기존 restart 2도 증가하지 않았다. 새 API의 실행 image ID/revision/version이 후보와 일치했고 OOM=false였다.
- 인증 H3 세 endpoint(bot health, llm internal readiness, admin health)가 모두 성공했다. 시작 이후 stdout/stderr에서 ERROR/FATAL/PANIC·SQLSTATE·permission denied·x509·파일 부재·OOM 표지는 0건이었다. PostgreSQL·cache 초기화 표지는 관측됐고 외부 CLIProxy 생성 호출은 실행하지 않았다.
- 10:24 UTC guarded DB: primary, DB 20,048,320,191 bytes, invalid index 0, lock wait 0, 5분 초과 transaction 0, network DB 접속 27/27 TLS, 적용 migration 118개. queue snapshot은 PROCESSED 67,106행이며 nonterminal은 0이었다.
- 10:25:59 UTC Prometheus: API up=1, consume success=107, oldest queue age=0. retention histogram count가 2→4로 증가해 시작 시와 다음 120초 주기의 projection/source 실행을 확인했다. 새 retention error series는 관측되지 않았다. 첫 source 정리에서 application 714·source 187·absence 99·queue 96행 삭제가 관측됐다.
- **효과 한계:** 사후에도 7일 cutoff를 넘긴 RETIRED projection은 0개였다. 따라서 운영에서 64배치 대량 삭제 성능을 실측한 것은 아니며 해당 근거는 앞의 격리 PG 회귀다. TTL 안의 전체 snapshot 쓰기와 이미 할당된 공간은 남으므로 즉시 DB 크기 감소나 40%/30% 목표 달성을 선언하지 않는다.
- 복구점: `/opt/hololive-bot/compose/rollbacks/retention-20261003-43a5c57a1`의 기존 배포 파일 archive·API VERSION, `hololive-api:rollback-retention-20261003-43a5c57a1` → 이전 image `sha256:c8457322a58d3c50e4c5275decd1097feda6cf6980482fbfcc58e8afae493b99`. archive SHA256 `d48996937c376a8809f45a8200d03590d9bfed2c0ca1afe74cd44000701f00d4`. 이전 API image와 VERSION을 함께 복원할 수 있으며 만료 삭제된 데이터는 이미지 롤백으로 복원되지 않는다. 보존물을 삭제하지 않았다.
- 원 영수증: `/home/kapu/.cache/tmp/hololive-db-growth-20261003-CxCeTv/`. secret·업무 payload는 보존하지 않았다. remote staging은 `/var/tmp/hololive-retention-43a5c57a1`이다. 원격 Git 게시·tag/release·기본 checkout 통합은 미실행이다. Fallback delta: none.
