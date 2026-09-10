# 관리자 교체 출시 실행 기록

2026-09-10. `PLN-20260909-hololive-admin-bigbang-replacement` T10/T11, `DEC-20260909-hololive-admin-bigbang-replacement`, `DEC-20260910-hololive-admin-performance-release-exception`.

## 승인 범위와 성능 예외

사용자는 관찰된 RSS 회복 실패를 수용하고, 적대적 리뷰 뒤 현재 관리자 교체를 커밋·푸시·운영 반영하도록 승인했습니다. 이전 장시간 대기 축소 지시도 유지합니다. 정규 12회/60분 결과를 새로 통과로 만들지 않으며, 4회 비교·10분 관찰과 [기존 실패 원본](performance-resolution.md)을 보존합니다. GC와 OS 메모리 보유 영향은 진단 근거가 있지만, RSS 증가 전체의 원인이나 누수 부재를 확정하지 않습니다.

대상은 `hololive-bot`의 관리자 교체 변경과 관련 메타 저장소 기록, 각 저장소의 `origin/main`, 중앙 `hololive-osaka`의 `admin-dashboard`·`admin-docker-proxy`입니다. 첫 세대 전환에 필요한 관리자 ingress 정비/개방, 기존 source 제한 적용, 관리자 세션 폐기와 새 signing secret, 이미지/배포 파일 전송·설치 및 필요한 재생성을 포함합니다. 업무 API·알람 worker·collector·업무 DB의 동작이나 데이터를 변경하지 않습니다. 기존 password hash·Holo API key·Valkey credential은 유지합니다.

순서는 리뷰·수정 → 필수 검사·커밋·푸시 → 깨끗한 검토 revision의 로컬 arm64 빌드와 검증 → 구형 복구 artifact 보존·전송 → source 제한 확인 → 관리자 정비·구형 종료·세대 전환 → smoke·개방·300초 관찰입니다. 실패 시 owning runbook의 정비 유지·전체 rollback 경계를 적용하며 불명 효과를 재실행하지 않습니다.

## 현재 증거

- [적대적 리뷰](release-review.md): P2 I/O 원인 유실 1건 수정, 미해결 출시 차단 결함 없음. 부모가 Holo adapter 전체 검사를 실행해 통과했습니다.
- PR [#483](https://github.com/park285/hololive-bot/pull/483)은 모든 필수 검사를 통과하고 `3f9e625c72bf4937223608c05bf44a5c945883a0`으로 main에 반영됐습니다. 로컬 게시 gate의 Go build/test/race/lint/NilAway와 프런트 175건·3개 엔진(skip 0), 최신 취약점 실제 호출 경로 0건을 확인했습니다. GitHub 실행 `34465125970`의 모든 job과 fast-gate도 통과했습니다.
- main의 전체 source tree가 검토·게시 검사한 `fc4c715df600233964e15975db9cc2b8c4e6fa6a`와 같습니다. 로컬에서 만든 두 아키텍처 이미지는 `49abf20fc…`의 실제 검증 이미지와 전체 RootFS 및 revision label을 제외한 전체 Config가 같습니다. 원본 시험의 candidate ID는 바꾸지 않고 [동일성 근거](release-image-equivalence.json)와 [통합 검증](release-verification.json)에 연결했습니다. 최종 arm64 image는 `sha256:d22b9b83465e04f4c5976b4a2f1f71d699592827e43003dc7fb9bccb9e9c68ef`입니다.
- ARM 격리 경계 111건, 구형 전체 artifact와 전환·복구 9단계 및 300초 상한 내 60회 관찰·유효 WS 147개, production 브라우저 24개 사례·자산 신형 58개/구형 39개, 초기 JS 12개 표본이 통과했습니다. JS 증가율은 9.74%입니다. 최종 두 image의 SBOM은 각각 997개 component이며, 원본과 hash는 통합 검증에 있습니다. 단축 native 비교 4회의 RSS 중앙값은 61,820,928→35,411,968 bytes, 7개 route p95는 +15% 이내였습니다. 이는 정규 장시간 회복 검사 통과를 뜻하지 않습니다.
- 시작 시 중앙 `admin-dashboard`는 healthy이며 실제 image는 `sha256:17b56c3a48b055fdab3d60691ec40e90de1f32e65691e1be0718b316b84e4fb7`, revision `6ba2ede142501eb5bc436f9d1af2587a3f1d81b0`입니다. ingress source 제한 service는 disabled/inactive입니다. 이 상태는 현재 배포 준비에서 해소할 항목입니다.

중앙에 검증된 image·10개 배포 파일을 적재하고 구형 image와 배포 파일·ingress 설정·proxy 명령을 root 전용 복구 경로에 보존했습니다. [전송·보존 기록](release-stage.json). 기존 source 제한 service를 enable/start한 뒤 실제 nft table, Seoul source의 두 포트 200과 kapu source의 두 포트 TCP 거부를 확인했습니다. [방화벽 증거](release-firewall.json).

## 운영 전환 결과

2026-09-10 19:34:24 KST에 관리자 origin을 503으로 닫고 구형 BFF를 즉시 정지했습니다. 246ms 뒤 exit 0·OOM 없음·listener 종료를 확인했습니다. 전환 직전 host의 30190 established 연결은 0개였고, 종료 구간의 mutation 감사 사건은 0건입니다. 관리자 prefix만 SCAN 1회로 폐기했으며 당시 삭제 대상은 0개였습니다. 업무 요청을 재생하지 않았습니다.

정본의 `SESSION_SECRET`만 새로 발급하고 나머지 파일 바이트를 보존했습니다. 좁은 host/stack dry-run 뒤 sync하여 정본·mirror 일치와 manifest 24행을 확인했고, runtime 파일 4개의 root:1002·0640을 확인했습니다. `stack-secrets-20260910T103525Z.tar.gpg` 암호화 백업은 기존 Seoul 목적지에 전송됐습니다. 시크릿 값이나 시크릿 파일 hash는 보고서에 넣지 않으며, 이번 백업의 복호화 리허설은 실행하지 않았습니다. 기존에 수용된 kapu 단독 복호화 키 보관 범위는 바꾸지 않았습니다.

`prod → admin-security → live-compat` 순서에서 `up -d --no-build --no-deps admin-docker-proxy admin-dashboard`만 실행했습니다. 새 두 컨테이너의 시작 시각·image·정책과 BFF healthy·재시작 0을 확인했습니다. 정비 상태에서 health 200, metadata no-store·동일 세대, generation 없는 요청 409와 익명 session/WS 401을 확인한 뒤 19:36:51 KST에 origin을 개방했습니다. Nginx 기존 worker 종료·관리자 200·short-link 200도 확인했습니다.

19:39:22~19:44:17 KST의 295.365초 동안 5초 간격 60개 표본에서 공개 health·metadata, BFF/proxy, short-link가 모두 정상이며 재시작·OOM은 0입니다. 프로세스 RSS 범위는 27,328,512~29,614,080 bytes였습니다. 이 짧은 운영 관찰은 면제된 장시간 회복 검사를 대체하지 않습니다. 확인한 다른 6개 서비스의 ID·image·시작 시각·재시작 횟수는 전환 전과 같습니다. 재생성 이후 두 관리자 서비스의 구조화 error 및 panic/fatal 계열 표지는 0건입니다. [실제 전환·관찰 원본](release-cutover-live.json).

첫 전환 직후 공개 origin의 로그인 화면을 실제 Chromium으로 열어 입력·로그인 버튼과 자산 15개의 200, page error·CSP 위반 0건을 확인했습니다. 당시 로그인 제출은 0회이며 인증된 동작의 시험으로 사용하지 않습니다. [공개 화면 검증](release-public-login.json). 이후 아래 임시 계정 지원을 배포하여 실제 로그인·조회·실시간 통계를 검증했습니다.

첫 전환 image의 source revision은 위 `3f9e625c7…`이며, 현재 운영 source는 임시 계정 지원을 추가한 `be4a48737036000ba8209340458c38921c2480a6`입니다. 후속 문서·증거 HEAD를 실행 image revision으로 바꾸지 않습니다. 구형 복구 tag와 root 전용 deploy backup은 유지합니다.

실제 로그인 인수를 완료했으며 성능 예외 외의 출시 조건을 추가로 면제하지 않습니다. 기존 RSS 회복 실패는 그대로 보존합니다.

## 임시 계정을 통한 운영 로그인 인수

운영 정본에는 password hash만 있어 첫 전환의 개방 전에 새 로그인·인증된 CSRF·WS를 확인하지 못했습니다. 사용자의 후속 요청에 따라 기존 관리자 암호를 유지하면서 CLI가 만료형 조회 계정을 발급·폐기하도록 구현했습니다. `DEC-20260910-hololive-admin-temporary-test-account`, PR [#486](https://github.com/park285/hololive-bot/pull/486), [구현·발행·실제 운영 인수 기록](test-account-validation.md)이 근거입니다.

23:52 KST에 중앙 `admin-dashboard`만 위 source의 검증된 arm64 image로 반영했습니다. 15분 임시 계정으로 실제 로그인과 7개 메뉴의 11개 GET, CSRF 403/200, WS 2개 frame을 확인했습니다. 계정 폐기 뒤 동일 cookie 401·재로그인 401·WS 1008, 계정 부재와 자격증명 파일 제거를 확인했습니다. 업무 mutation·page error·CSP 위반은 0건입니다.

원래 관리자 비밀 파일과 다른 7개 컨테이너 상태는 전환 전과 같고, BFF healthy·재시작 0·OOM 없음·오류 표지 0건입니다. 첫 교체의 세션 prefix purge나 signing secret 회전은 반복하지 않았습니다. 이 실제 검증으로 T11/AC11/V11을 완료하며, 계획은 사용자가 수용한 기존 성능 예외를 보존해 종료합니다.
