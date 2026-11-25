# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **AP a native 창 정리:** PgBouncer 퇴역 전환이 끝나 W1·W2 창을 지웠습니다. native 도구는 W3에서 수용된 원점만 받습니다. 퇴역 전환 절차 문서와 그 링크도 지웠습니다.

## r1.2.4 - 2026-11-25

- **AP a 수용 readiness:** W3 수용 단계는 issuer가 준비된 뒤 collector `/ready`가 한 번 503을 돌려주면 바로 결과 불명으로 끝났습니다(2026-11-24 osaka1 정산 재개, youtube.js helper 응답 지연). 이제 기존 90초 한도 안에서 다시 확인하고, 한도가 지나면 지금처럼 결과 불명으로 끝납니다.
- **AP a 수용 단계 정산:** `--settle`은 수용이 차단 정책을 확정하기 전(정책 `observing`, `accepted-window` 없음)에 결과 불명으로 끝난 창도 받아들이고, 관찰 기록·인계·`accepting`을 함께 보관한 뒤 다시 재개합니다. issuer가 다시 기동하지 못해 socket이 `trigger-limit-hit`으로 실패했으면 그 실패 기록을 확인하고 socket을 비활성으로 되돌립니다.

## r1.2.3 - 2026-11-25

- **AP a 정지 창 재개 관찰:** issuer가 정상 교체되는 순간 systemd 상태를 나눠 읽다가 `activating`과 `running`이 엇갈리면 관찰을 결과 불명으로 끝내던 경합을 고쳤습니다(2026-11-24 osaka1 W3 재개). 상태가 바뀌는 중이면 기존 재시도 한도 안에서 다시 확인하고, 같은 상태에 머물면 지금처럼 실패합니다.
- **AP a 결과 불명 정산(`--settle`):** 후보 collector 기동 뒤 관찰만 결과 불명으로 끝난 W3 창은, 세 unit의 정지와 정지 완료 기록을 확인한 뒤 결과 표식과 1차 시도 영수증을 창 안에 보관하고 같은 후보로 다시 재개·수용할 수 있습니다. 다른 결과 불명이나 판정 가능한 실패는 지금처럼 재개하지 않습니다.
- kapu d custody tar 안내에 두 `.container`가 참조하는 `hololive-collector.network`·`hololive-youtube-po-d.volume`을 넣었습니다.

## r1.2.2 - 2026-11-24

- **`!달력` 플랫 포스터 테마 6종 추가·7종 페이지별 독립 랜덤 회전:** 기존 다크(`starlight_orbit`)의 그라데이션·글로우·반투명 장식을 걷어내고 `Alpha=255` 솔리드 색지·`136px×7열` 정수 격자·`BlackHanSans` 조판 기반 플랫 포스터 테마 6종(다크 3종 `cream_bromide`·`kurokami_risograph`·`holo_stage`, 라이트 3종 `shirokami_risograph`·`sakura_sora_press`·`museum_archive`)을 추가했습니다. 기존 라이트(`prism_daylight`) 1세트와 합쳐 총 7종 테마 풀을 다크·라이트 구분 없이 운영하며, 생일 달력(1/2)과 데뷔 기념일 달력(2/2)이 각자 직전 테마만 피해 독립적으로 테마를 뽑으므로 `라이트+다크`·`다크+라이트`·`라이트+라이트`·`다크+다크` 모든 조합이 허용됩니다. 메모리(상한 64장)와 공유 Valkey 캐시(`bot:calendar:album:v7:<YYYY-MM>:<kind>:<theme>`)를 페이지 종류·테마 단위로 분리해 교차 조합 시에도 이미 렌더된 페이지를 재사용합니다.
- **PO 검증 쌍 전송:** kapu의 rsync가 3.5.0으로 올라간 뒤 `po-central-cutover.sh`의 `--files-from=<(...)`가 `/dev/fd/63`을 열지 못해, c `quiesce-verified`와 deploy 전송이 원격 단계에 들어가기 전에 실패했습니다. 파일 목록을 표준 입력(`--files-from=-`)으로 넘깁니다.
