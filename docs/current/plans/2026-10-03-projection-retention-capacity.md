# 수집 대상 이력 정리 처리량 보완

운영 revision `5a3f03776` 기준의 독립 worktree에서 구현한다. 기본 checkout의 다른 작업은 보존한다.

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
- **미실행:** 커밋·원격 게시·운영 배포·운영 데이터 변경. 기본 worktree 통합도 하지 않았다. 운영 적용에는 API 배포 승인이 필요하고, Git 게시 승인은 별도다.
- **남은 한계:** 전체 snapshot 재생성의 쓰기 증폭은 남는다. 7일 TTL 안의 이력 증가와 파일의 빈 공간은 이 수정으로 즉시 줄지 않는다. 운영 처리량·DB 정상 상태 용량과 기존 40%/30% 목표 달성은 미확정이다.

Fallback delta: none. 오류 무시·추가 재시도·retention 기간 축소는 없다.

## 운영 반영 재개

후속 사용자 진행 승인으로 중앙 Hololive API 7.2.3의 로컬 release commit·전체 local CI·ARM64 image 빌드·전송·API 단독 재생성·사후 검증을 수행한다. Git 원격 게시와 다른 runtime 교체는 포함하지 않는다. 현재 API revision은 기준 `5a3f03776`과 일치하며 worker는 별도 후속 revision으로 바뀌었으므로 기존 worker와 deploy tree는 보존하고 API image와 API VERSION만 전환한다. 새 migration이 없어 ledger는 조회·대조만 한다.
