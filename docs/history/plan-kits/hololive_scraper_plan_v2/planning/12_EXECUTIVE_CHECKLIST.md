# Executive Checklist

## 개발 시작 전

- [ ] baseline test 통과
- [ ] current main 기준 파일 확인
- [ ] PR-01부터 순서대로 진행
- [ ] browser/OCR은 마지막까지 보류

## 각 PR merge 전

- [ ] 해당 phase test 통과
- [ ] channel_id metric label 없음
- [ ] snapshot default OFF
- [ ] browser default path 아님
- [ ] 403/429 retry 정책 유지
- [ ] events==0 success 유지
- [ ] quota check 유지
- [ ] rollback env 확인

## 운영 배포 전

- [ ] Stage 1 taxonomy만 배포
- [ ] reason별 metric 확인
- [ ] channel health dry-run
- [ ] snapshot 제한 설정
- [ ] browser diagnostic 수동만 허용
- [ ] active tiering은 마지막

## 장애 시

- [ ] 403/429: RPM/global hard backoff 확인
- [ ] parser_drift: snapshot/fixture/parser test
- [ ] timeout/transport: proxy/network
- [ ] quota: fallback 대상/empty upcoming 오분류
- [ ] disk: snapshot off/cleanup
- [ ] latency: channel health/tier rollback
