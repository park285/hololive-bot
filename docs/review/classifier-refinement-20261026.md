# 방송 유형 분류기 3차 보완

[2차 보완](classifier-unclassified-20260911.md) 이후 쌓인 방송으로 미분류를 다시 측정하고
방송 유형 규칙을 보완했다. 규칙 버전은 `2026-10-26.1`이다. 진행자 판별(`mekparkhost`)은
바꾸지 않았다.

## 데이터와 비교 범위

`hololive-seoul` (`100.100.1.8`)의 `holo-postgres`/`hololive`를 조회했다. 세션에
`default_transaction_read_only=on`, `statement_timeout`, `lock_timeout`을 적용하고 같은 세션에서
`SHOW transaction_read_only = on`을 확인했다. 사용자·채팅방·메시지·인증 정보는 수집하지 않았다.

```text
captured_at=2026-10-26T14:00:31.702318+00:00
```

- 입력: 종료된 세션 전체 10,807건(2026-09-11 이후 3,366건)의 영상·채널 ID, 제목, 주제.
  빈 세션 주제는 방송 이력 repository와 같이 최신 LIVE dispatch 이벤트의 주제로 보완했다.
- 2차 보완 이후 VSPO 채널과 신작 게임 방송이 늘면서 9월 11일 이후 미분류가 14.6%였다.
  전체 미분류 1,056건은 주제가 없는 제목 753건과 사전에 없는 Holodex 주제 303건이었다.

| 비교 | 변경 전 | 변경 후 |
|---|---:|---:|
| 전체 10,807건의 미분류 | 1,056건 (9.8%) | 576건 (5.3%) |
| 2026-09-11 이후 3,366건의 미분류 | 493건 (14.6%) | 128건 (3.8%) |
| 기존 유형이 바뀐 방송 | - | 24건 |

미분류 감소는 판별 범위의 개선이며 실제 영상 독립 검수 정확도가 아니다. 공개 메타데이터
조회, 영상 재생 검수, 영상별 검토(`reviewed_videos`) 추가는 하지 않았다.

## 변경한 판정

- **게임 주제를 제목 태그에도 적용:** 게임으로 매핑한 Holodex 주제 ID(`_`를 공백으로 바꾼 이름)가
  첫 제목 태그의 시작에 단어 경계로 오면 게임으로 본다. 기존에는 `forza`·`dorfromantik`·
  `heartopia`가 주제 사전에 있어도 주제 없이 `【Forza Horizon 6】`만 있는 방송은 미분류였다.
  태그 중간이나 태그 밖의 같은 단어는 인정하지 않는다. 주제 ID의 끝 마침표는 떼고, 곡선
  아포스트로피(`’`)는 `'`로 정규화한다. 이 변경만으로 기존 판정 변화 없이 30건이 게임이 됐다.
  기존 주제를 포함한 게임 주제 전체가 태그 이름이 되므로 `convenience store`·`heavy rain`·
  `persona`처럼 일반어인 주제가 첫 태그를 시작하면 게임이 된다. 10,807건에서 그런 오분류는
  없었고 제외 목록은 두지 않았다.
- **게임 주제 68개 추가:** 관측 제목이 게임명을 명시한 주제만 매핑했다. `drawing`·`announce`·
  `morning`·`celebration`·`ranking`·`teaser`·`charity`는 방송 형식이 섞여 계속 매핑하지 않는다.
  `anniversary`를 이벤트로 매핑하면 `歌枠`과 `#昼ホロ` 뉴스 방송이 이벤트로 바뀌어 제외했다.
  `duolingo`는 기존 `language_learning`과 같이 잡담, `superchat_reading`은 잡담이다.
- **제목 사전:** VSPO의 `VALO`·`#LTK`·`TFT`·`スクリム`·`ぶいすぽGG`·m HOLD'EM과 신작 게임명,
  `(official)`·`【original】`·`cover` 노래, `録画視聴`·`切り抜き鑑賞`·`一緒に観よう`·`」を観よう`
  동시시청, `MEMBERSHIP ONLY`, 인사말 잡담(`おはよ`·`good morning`·`こんばんは`)과 반복 잡담
  시리즈, `#holoctober`·`Legend of Polka` 기획을 추가했다. 인사말과 단독 `cover`는 generic
  규칙이라 주제나 첫 태그의 게임명을 이기지 않는다. 다만 기존 `雑談`·`おはよう`처럼 이 단어가
  첫 태그 안에 있으면 본문의 게임명보다 먼저 정한다. 10,807건에서 이런 첫 태그는 모두 실제
  커버곡·의상 공개·잡담이었다. 일반 영어인 `a little to the left`는 첫 태그에서만 인정하고,
  `uncharted`는 게임 주제의 태그 이름에 맡긴다.
- **기존 오류 교정:** `sc ketchup`(슈퍼챗 밀린 읽기)이 게임 사전에 들어 있어 잡담 방송 4건이
  게임이었다. 잡담으로 옮겼다.

검토 중 세 후보를 버렸다. `MV公開`는 「大掃除 … 最後MV公開あり」처럼 끝에 MV를 공개하는 비노래
방송을 노래로 바꿨고, 강한 이벤트 키워드 `new outfit`은 `【TALK】… In new outfit`을 이벤트로
바꿔 generic 이벤트로 낮췄다. 강한 동시시청 키워드는 게임 주제도 덮어쓰므로 일반 동사
`観よう`와 단순 언급 `手描き切り抜き` 대신 작품을 보는 표현만 남겼다(`【Minecraft】星空を観よう`는 게임).

기존 유형이 바뀐 24건은 모두 직접 확인했다. 새로 매핑한 게임 주제가 제목의 잡담·기타 generic
근거(`#生スバル`, `朝活`, `#sponsored`)보다 우선하고, `切り抜き鑑賞` 방송은 게임·기념 이벤트에서
동시시청으로, `【Membership Only】` 방송은 멤버십으로, 슈퍼챗 주제 방송은 잡담으로 바뀌었다.
새로 분류한 480건 중 주제 없는 제목 기반 판정 전부와 비게임 판정 전부를 확인했다.

## 남은 미분류

9월 11일 이후 남은 128건 중 109건은 주제가 없고 제목 단서가 부족하다(`a`, `にょ`, `休憩`,
`【EN】` 등). 19건은 위에서 매핑하지 않은 주제와 `Rubinite`·`My_Time`처럼 제목만으로 게임 여부를
확정하지 않은 주제다.

## 검증

로컬 kapu, Go 1.27.3 linux/amd64에서 수행했다.

```bash
env -u TEST_DATABASE_URL go test -race -p 2 -count=1 ./hololive/hololive-api/internal/planes/bot/...
golangci-lint run -c .golangci.yml ./hololive/hololive-api/internal/planes/bot/internal/command/handlers/...
GOMEMLIMIT=10GiB go vet -p 2 -vettool=/home/kapu/go/bin/nilaway \
  ./hololive/hololive-api/internal/planes/bot/internal/command/handlers/...
```

- bot plane race 테스트 전체 통과, golangci-lint `0 issues.`, NilAway exit 0.
- 방송 회귀 표본은 130 → 180건이다. 신규 50건의 기대값은 제목·주제로 직접 지정했고 분류기
  출력에서 만들지 않았다. 관측 사례로 규칙을 보완했으므로 holdout이 아니다.
- 변경 전 규칙(`-overlay`)에서는 신규 표본 46건과 새 경계 테스트 6건이 실패하고, 나머지 표본
  4건과 경계 테스트 11건은 기존 동작 유지 가드로 통과했다.

Fallback delta: none. 오류 처리 경로, 의존성, 유형 이름, DB 스키마는 바꾸지 않았다.
