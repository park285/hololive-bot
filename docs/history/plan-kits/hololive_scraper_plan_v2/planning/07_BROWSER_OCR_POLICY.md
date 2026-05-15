# Browser and OCR Policy

## 1. 요약

브라우저 컨테이너는 넣을 수 있습니다. 하지만 기본 수집 경로로 넣으면 안 됩니다.

OCR은 운영 파싱 source로 사용하지 않습니다.

## 2. Browser를 허용하는 경우

- parser drift가 반복되어 raw HTML만으로 원인 파악이 어려운 경우
- YouTube가 server HTML과 rendered DOM을 다르게 내려주는 경우
- fixture로 승격할 rendered HTML이 필요한 경우
- 운영자가 특정 channel/page를 수동 진단하는 경우

## 3. Browser를 금지하는 경우

- 403/429 회피 목적
- CAPTCHA/login wall 우회
- every poll fallback
- API quota 절약을 위한 대체 수집 source
- OCR 결과를 stream/community/shorts domain model로 직접 변환

## 4. OCR을 넣지 않는 이유

OCR은 다음 문제를 만듭니다.

- locale에 취약합니다.
- font/thumbnail/text overlay에 취약합니다.
- 시간/제목 추출 정확도를 보장하기 어렵습니다.
- test fixture화가 어렵습니다.
- 운영 latency가 커집니다.
- 차단 우회 도구처럼 보일 위험이 있습니다.

## 5. OCR이 허용되는 유일한 경우

사람이 보는 진단 artifact에 보조 정보를 붙이는 경우입니다.

예:

```text
screenshot contains text-like regions: true
visible card count estimate: 3
```

금지:

```text
OCR로 읽은 제목을 Stream.Title로 저장
OCR로 읽은 시작 시간을 StartScheduled로 저장
OCR로 읽은 channel name을 ChannelName으로 저장
```

## 6. Browser diagnostic flow

```text
parser_drift 1회
  → raw snapshot만 저장

parser_drift 2회
  → channel/source health backoff 증가

parser_drift 3회 이상
  → browser diagnostic candidate

browser diagnostic 조건 충족
  → rendered HTML snapshot
  → optional screenshot artifact
  → parser fixture 승격
```

## 7. Browser service API contract

요청:

```json
{
  "url": "https://www.youtube.com/channel/UCxxx",
  "headers": {
    "User-Agent": ["..."],
    "Accept-Language": ["en"]
  },
  "screenshot": true,
  "wait": "network_idle",
  "timeout_ms": 20000
}
```

응답:

```json
{
  "status_code": 200,
  "html": "<html>...</html>",
  "screenshot_ref": "artifact://...",
  "header": {}
}
```

## 8. Browser rate limit

권장:

- max 5/hour
- max 1 concurrent
- parser drift 누적 채널만 허용
- 403/429 상태에서는 실행 금지

## 9. 운영 경고

Browser diagnostic이 늘어났다는 것은 정상 수집이 좋아졌다는 뜻이 아니라 parser가 깨지고 있다는 뜻입니다. browser 성공률을 KPI로 삼으면 안 됩니다. KPI는 parser fixture fix 후 `parser_drift` 감소입니다.
