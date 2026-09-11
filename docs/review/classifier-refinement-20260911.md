# 제목 분류기 데이터 보완 결과

이 문서는 1차 결과다. 남은 40건의 추가 검토와 최신 결과는
[2차 분류 보고서](classifier-unclassified-20260911.md)에 기록했다.

2026-09-11에 UNIT B/ACHRORA 진행자 판별과 방송 유형 판별을 보완했다.
기준 revision은 `48a3205bd106f754df76e33a54313826634ba7d6`이며 작업 브랜치는
`codex/classifier-refinement-20260911`이다. 작업 트리는
`/home/kapu/work/iris-stack/.tmp/classifier-refinement-20260911/hololive-bot`이다.

## 데이터와 비교 범위

`hololive-osaka` (`100.100.1.8`)의 `holo-postgres`/`hololive`를 조회했다.
모든 DB 세션에 아래 옵션을 적용하고 같은 세션에서 `SHOW transaction_read_only = on`을
확인했다. 사용자·채팅방·메시지·인증 정보는 수집하지 않았다.

```text
PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=15000 -c lock_timeout=1000
captured_at=2026-09-11T08:04:14.473075+00:00
```

- 방송 유형: 2026-08-29 00:00 KST 이후 종료된 세션 890건. 최대 3,000건으로 제한했다.
  제목과 주제를 입력으로 사용했으며 빈 세션 주제는 방송 이력 repository와 동일하게
  최신 LIVE dispatch 이벤트의 주제로 보완했다. 이 조회는 기존 입력 계약을 재현한다.
- 진행자: 두 채널의 세션과 영상 221건. 최대 400건으로 제한했고 중복 video_id는 세션을
  우선했다. 기존 회귀 표본 195건에 신규 26건을 추가했다. 세션에는 예정 방송도 포함된다.
- 방송 회귀 표본: 누락된 게임, 유형 충돌, 유지해야 할 방송 형식과 미상 사례를 제목·주제로
  검토한 99건이다. 변경 후 분류 결과에서 정답을 생성하지 않았다. 관측 사례에서 규칙을
  보완했으므로 이 자료는 미사용 holdout이나 무작위 정확도 평가가 아니다.

| 비교 | 변경 전 | 변경 후 |
|---|---:|---:|
| 전체 방송 890건의 미분류 | 230건 (25.8%) | 40건 (4.5%) |
| 검토한 방송 99건의 기대값 일치 | 38건 | 99건 |
| 진행자 표본 221건의 진행자·게스트 기대값 일치 | 220건 | 221건 |

미분류 감소는 판별 범위의 개선이며 실제 영상 독립 검수 정확도가 아니다. 저장된 제목은
최초 알림 이후 바뀌었을 수 있어 알림 시점 정확도도 추정하지 않는다.
남은 40건에는 단서가 부족한 제목과 미지원 약어·방송 표현이 함께 있으므로 추가 검토 대상으로
남긴다. 모든 미분류가 실제로 판별 불가능하다는 뜻은 아니다.

## 변경한 판정

진행자 판별은 기존 `mekparkhost`와 embedded `rules.json`이 계속 소유한다.
채널 진행자의 개인 태그·역할·시리즈·검증한 이름 쌍이 있으면 단순 성명 언급을 제외한다.
실제 `ijTWZNQgxg4`는 사야나와 리라라에게 편지를 쓰는 히나미 방송인데, 기존에는 세 명을
공동 진행자로 잡았다. 이제 두 수신인을 진행자나 게스트로 표시하지 않는다. 명시적 진행자
근거가 없는 성명 판별, 공동 진행자, 시리즈 게스트, 미상 구독의 기존 동작은 유지한다.

UNIT B의 공개 수익화 기념 릴레이 제목 세 건(`-Cm4Zjy4YgY`, `qliKuM5K2hg`,
`zSmwIonV55U`)에서 `ネオンの枠`·`ミラの枠`·`ライラの枠`를 확인해 역할 표기에 `の枠`를
추가했다. 원본에는 개인 태그도 있으므로 이 규칙이 실측 식별 수를 늘렸다고 계산하지 않는다.
태그를 제거한 변형은 별도 회귀 테스트로 검증한다. 인물 미상 20건은 그대로 보류한다.

방송 유형 판별은 기존 handlers와 embedded `broadcast_type_rules.json`을 유지한다.
기존 NFKC, 단어 경계, 주제 사전, 제목 규칙을 재사용했으며 의존성·유형 이름·DB 스키마를
바꾸지 않았다.

- 관측된 게임 주제와 제목을 추가했다. `ARK`·`RUST`·`PEAK`는 일반 문장과 구분하기 위해
  첫 제목 태그에서만 새 키워드를 적용한다. 알 수 없는 주제를 일괄 게임으로 지정하지 않는다.
- `hololive Dreams` 게임 방송 23건의 이벤트 오분류를 교정했다.
  [공식 게임 안내](https://www.hololive-dreams.com/)도 게임 성격을 확인하는 근거다.
  명시적인 대회 제목은 기존 이벤트 우선순위를 유지한다.
- 기획 설명회·팀 발표·게임 회고는 게임 주제를 추가한 뒤에도 각각 이벤트·뉴스·잡담으로
  유지한다. 게임 진행 중 단순한 회고나 공지 언급은 기존 게임 판정을 유지한다.
- `【MEMBERS】`가 있는 실제 방송 `V1_RIGJUYys`는 `talk` 주제보다 멤버 한정 제목을
  우선한다. 기존 멤버십 주제가 있으면 그 주제 출처를 유지한다.
- 작곡 주제, 낭독, 작업 방송, 엽서 서명 방송을 기존 유형에 연결했다. 게임이었던 기타·잡담
  오분류 2건도 교정했다.

전체 890건에서는 미분류 190건이 새 유형을 얻었고, 기존 유형 26건이 바뀌었다.
게임 단어만으로 잘못 매칭할 수 있는 일반 문장, 멤버십 충돌, 대회/플레이/회고 구분,
본문에 언급된 인물의 구독 필터를 회귀 테스트에 포함했다. Fallback delta: none.

## 검증

로컬 `kapu`, Go 1.27.1 linux/amd64에서 다음을 수행했다.

```bash
env -u TEST_DATABASE_URL go test -race -p 2 \
  ./hololive/hololive-shared/pkg/domain/mekparkhost/... \
  ./hololive/hololive-shared/pkg/service/youtube/outbox/format \
  ./hololive/hololive-alarm-worker/internal/service/dispatchrun \
  ./hololive/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter \
  ./hololive/hololive-api/internal/planes/bot/internal/command/handlers

golangci-lint run -c .golangci.yml \
  ./hololive/hololive-shared/pkg/domain/mekparkhost/... \
  ./hololive/hololive-api/internal/planes/bot/internal/command/handlers

GOMEMLIMIT=10GiB go vet -p 2 -vettool=/home/kapu/go/bin/nilaway \
  ./hololive/hololive-shared/pkg/domain/mekparkhost/... \
  ./hololive/hololive-api/internal/planes/bot/internal/command/handlers
```

전체 race 테스트는 6개 패키지가 통과했다. 최초 lint의 테스트 문자열 상수·공백 지적을
수정한 뒤 `0 issues.`를 확인했고, 수정한 두 패키지의 분류/구독 race 테스트도 다시 통과했다.
NilAway도 exit 0으로 완료했다. JSON 파싱·영상 ID 중복 검사와 최종 `git diff --check`가
통과했다.

새 의존성, 모델 학습, 운영 DB 쓰기, 발송, 커밋, push, 배포는 수행하지 않았다.
운영에 반영하려면 변경 검토·통합 후 별도 배포가 필요하다. 현재 ML 실험의 과거 결과는
9월 6일 당시 입력/규칙에 대한 결과이며 최신 회귀 표본의 정확도를 뜻하지 않는다.

## 재현 자료

고정 회귀 자료는
[진행자 표본](../../hololive/hololive-shared/pkg/domain/mekparkhost/testdata/title_corpus.json)과
[방송 유형 표본](../../hololive/hololive-api/internal/planes/bot/internal/command/handlers/testdata/broadcast_type_corpus.json)이다.
위 race 테스트는 DB 스냅샷 재조회 없이 두 표본을 검증한다.

작업 트리의 `.tmp/classifier-audit/`에는 조회 당시 입력, 변경 전후 예측과 집계가 있다.
일회성 방송 비교 코드는 `hololive/hololive-api/internal/planes/bot/.tmp/classifier-audit/main.go`에
있으며 운영·검증 패키지에는 포함하지 않는다.

```text
db-snapshot.json          316ac65808f928ac0526d3c02b04f154adb297f3b38dadbd8086840c3d59848d
broadcasts-before.jsonl   37ba99efc323d6707578a2fb58464c3a20626da4149b8d3caaa4218ca3140086
broadcasts-after.jsonl    080f4daa4eef95f4acd34ed472f7ed155ae666dba7e92f24bf051f6da62bebf7
```
