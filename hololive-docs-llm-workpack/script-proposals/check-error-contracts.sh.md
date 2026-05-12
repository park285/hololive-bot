# Proposed script: check-error-contracts.sh

목적: internal API error code가 문서 및 contracts에 등록되어 있는지 검사합니다.

단계적 도입:

1. warning mode: raw error string 사용 위치를 출력합니다.
2. fail mode: contracts에 없는 error code를 사용하면 실패합니다.

초기 검색 대상:

- `RespondError(c, ..., "<raw>", ...)`
- `strings.Contains(err.Error(), "status`
- `status 404` 문자열 분기
