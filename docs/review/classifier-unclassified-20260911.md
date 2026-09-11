# 미분류 40건 추가 검토와 분류 적용

[1차 보완](classifier-refinement-20260911.md)에서 남았던 방송 40건을 모두 분류했다.
동일한 890건 표본의 미분류는 230 → 40 → 0건이다. 실제 공개 자료를 검토한 결과는
[40건의 제목·유형·근거 JSON](classifier-unclassified-20260911.json)에 기록했다.

| 최종 분류 | 건수 |
|---|---:|
| 잡담 | 9 |
| 게임 | 8 |
| 노래 | 2 |
| 멤버십 | 3 |
| 이벤트 | 3 |
| 뉴스/정보 | 1 |
| 기타/기획 | 14 |

## 확인 근거와 적용 방식

기존 collector-owned YouTube.js 18.0.0으로 공개 메타데이터 60건을 조회했다. 남은 방송
40건과 진행자 미상 표본 20건에 한정했고, 로그인·쿠키·운영 인증 정보를 사용하지 않았다.
각 요청은 20초, 동시 조회는 3건으로 제한했다. 기본 메타데이터 조회 성공과 콘텐츠 재생
가능 여부는 별개다. 필요한 사례에서 공개 썸네일·접근 제한 사유·시청 후기도 추가 확인했다.

29건은 재사용 가능한 제목·주제 규칙으로 분류했다. 인도네시아어의 `BERDINKUM`,
`Monday Morning/Malam Malas`, 일본어 잡담 약칭, 낭독·시식·촬영·박물관 상품 소개,
신규 게임명을 반영했다. `Voice_Mimicry_Show`는 영상 설명이 `声マネキング` 플레이를
명시하므로 게임으로 분류한다. 게임 방송 중의 단순 대회 언급, `雑貨`, 영문 단어 일부가
다른 방송 유형에 잘못 매칭되지 않는 경계 테스트도 추가했다.

제목만으로 구분하기 어려운 11건은 `broadcast_type_rules.json`의 `reviewed_videos`에
영상 ID·채널 ID·검토 시각·원본 URL·근거와 함께 기록했다. `ClassifyBroadcastVideo`는
두 ID가 일치하고 기존 판정이 미분류일 때 이를 사용한다. 확인된 멤버 전용 영상은 다른
일반 유형보다 우선한다. 다른 영상이나 같은 제목의 새 방송으로 결과를 확대하지 않는다.
이 경로를 방송 이력의 공통 row 판독에 연결해 단건 조회와 유형별 목록 필터에 함께 적용했다.
원본 제목과 주제는 수정하지 않는다.

- `sY3oRzO5JtQ` 「夏が終わる」: 공개 댓글 20건 중 17건이 잡담을 명시했다. 기본
  Gaming 카테고리와 달랐으므로 잡담으로 검토했다. 원본 영상 전체를 재생 검수한 것은 아니다.
- `seZvn7zCsAE` 「４時だってよ」: 공개 댓글 20건 중 6건이 잡담·상담을 명시하고,
  다른 후기와 People & Blogs 카테고리도 부합한다.
- `raD2SDEffE8`, `5khIfZ6FmkI`, `SrYExB6XHg8`: YouTube player가 멤버 전용 콘텐츠임을
  명시했다. Music/Gaming 카테고리나 동시 시청 제목보다 멤버십을 우선한다. 팬 이름인
  `WINGMEN ONLY`는 일반 멤버십 키워드로 등록하지 않았다.
- `ksf9tm7eyI4`와 `_IY8xsvkPf8`: 공개 썸네일에 각각 그림 방송과 잡담이 명시되어 있다.
  모호한 제목의 계절 표현을 일반 분류 규칙으로 만들지 않았다.

이외에 기존 `other`였던 SlashZero 게임 방송 2건도 교정했다. 카페 프로그램의 신규 규칙을
적용하면서 전날의 실제 게스트 방문 이벤트가 기타로 바뀌는 회귀를 발견했고, 명시적인
`リアル凸待ち` 근거를 이벤트로 유지하도록 수정했다.

공유 진행자 분류기에는 실제 공개 제목·설명의 개인 클립 태그
`#きりとりらら`·`#さやなカット`·`#ぴよつまみ`를 추가했다. 개인 이름을 제거한 변형에서도
해당 태그로 식별되는 것을 확인했다. 기존 진행자 표본 221건의 판정은 유지하며, 단체 소개나
공동 음악 제작 크레딧을 개인 한 명의 진행 근거로 사용하지 않는다.

## 커밋 전 리뷰

리뷰에서 다음 두 회귀를 테스트로 재현하고 수정했다.

- `#宵凪ネオン×清澄ライラ`처럼 한쪽 이름에만 해시태그가 있는 합방에서 라이라와 해당
  멤버 구독이 누락됐다. 성명 사이의 `×`를 명시적 합방 근거로 처리하며, `A×Bの曲`처럼
  다른 사람들의 곡을 인용한 문장은 제외한다. 이름 쌍이 없는 제목에는 추가 쌍 검사를 하지 않는다.
- 멤버십 개설 안내나 `gym membership`이라는 본문 언급이 멤버 전용 방송으로 분류됐다.
  제한을 명시한 표현은 유지하고, 단독 `membership`·`メンバーシップ` 표기는 정확한 첫
  제목 태그일 때만 인정한다. 실제 UNIT B의 멤버십 개설 안내는 뉴스로 분류한다.

수정 후 890건의 기존 유형 결과와 미분류 0건이 유지됐다. 새 회귀 검증은 공유 진행자 표시,
구독 필터, 멤버십/뉴스/잡담 구분을 포함하며 규칙 버전은 `2026-09-11.3`이다.

## 유지와 검증

검토 기록의 소유자는 bot plane의 방송 유형 분류기다. 새로운 명시적 주제·제목이 들어오면
일반 판정을 우선하되, 멤버 전용 검토 기록은 접근 범위가 바뀌면 재검토·수정·삭제한다.
다른 영상의 보정 항목도 공개 정보가 바뀌거나 반대 근거가 확인되면 다시 검토한다.
새 영상에 대한 자동 추측이나 운영 중 추가 외부 조회는 도입하지 않았다.

Go 1.27.1, 로컬 kapu에서 검증했다.

- 관련 6개 패키지의 전체 `go test -race -p 2`가 통과했다. 대상과 실행 방법은 1차 보고서와 같다.
- 고정 방송 표본 130건, 진행자 표본 221건의 기대값이 모두 일치했다.
- 격리된 PostgreSQL에서 영상별 검토가 단건 조회와 `type=talk` 목록 필터에 적용되며,
  원본 제목·빈 주제를 유지하는 것을 확인했다.
- 영상/채널 불일치, 일반 판정 우선순위, 멤버십 우선순위, 손상된 검토 기록의 거절을 검사했다.
- golangci-lint는 최초 상수·공백 지적 수정 후 `0 issues.`, NilAway는 exit 0이었다.
- 저장소 구조 검사 `--mode hard`가 통과했다. 진행자/게스트 배정 조건을 멤버 판정에
  모아 `Identify`의 복잡도를 상한 이내로 정리했으며, 변경 파일에는 advisory 5건이 남는다.
- JSON 무결성 및 최종 diff를 확인했다. Fallback delta: none. 오류 처리 경로는 바꾸지 않았다.

수치는 고정 표본에서의 분류 결과와 회귀 검증이다. 미래 방송 전체의 정확도나 독립적인 영상
재생 검수 정확도를 뜻하지 않는다. 시청 후기를 근거로 한 두 사례는 해당 근거 종류를 명시했다.

작업 트리는 `/home/kapu/work/iris-stack/.tmp/classifier-refinement-20260911/hololive-bot`,
브랜치는 `codex/classifier-refinement-20260911`이다. 소스 변경은 로컬 `main` 통합 대상으로
검토·검증했다. 운영 DB 쓰기·발송·push·배포는 수행하지 않았다.

## 재현 자료

전체 입력·예측은 작업 트리 `.tmp/classifier-audit/`에 있다. 저장소의 방송 회귀 표본은
외부 조회 없이 `TestClassifyBroadcastCorpus`로 검증할 수 있다.

```text
public-metadata.json   41d3cc29086c355b57dc1810051b950c254eebe2dfb83fc3c3bc6d87bd7a71c5
broadcasts-round2.jsonl f4c2760f5a9f19e9fa1afb6968f05e10a3b5deb98a57deab623a99e5af95a8bd
broadcast_type_corpus.json bc36caa68f1cda5e1838f8ac213c6863788389a3c61e0b7bbb6facc382998aea
```

## 영상별 결과

| 영상 | 분류 | 적용 |
|---|---|---|
| [JstSA5yRsj8](https://www.youtube.com/watch?v=JstSA5yRsj8) | 잡담 | 영상별 검토 |
| [Nwrl5RGOHsI](https://www.youtube.com/watch?v=Nwrl5RGOHsI) | 기타/기획 | 규칙 |
| [HQx1pVAkQMw](https://www.youtube.com/watch?v=HQx1pVAkQMw) | 게임 | 규칙 |
| [9r7QBE2gvLI](https://www.youtube.com/watch?v=9r7QBE2gvLI) | 기타/기획 | 규칙 |
| [pgjz49AXmvo](https://www.youtube.com/watch?v=pgjz49AXmvo) | 잡담 | 규칙 |
| [qMxeqbCClQY](https://www.youtube.com/watch?v=qMxeqbCClQY) | 잡담 | 영상별 검토 |
| [SrYExB6XHg8](https://www.youtube.com/watch?v=SrYExB6XHg8) | 멤버십 | 영상별 검토 |
| [4j-QEUM52fI](https://www.youtube.com/watch?v=4j-QEUM52fI) | 기타/기획 | 규칙 |
| [raD2SDEffE8](https://www.youtube.com/watch?v=raD2SDEffE8) | 멤버십 | 영상별 검토 |
| [wFkI7hRhJGs](https://www.youtube.com/watch?v=wFkI7hRhJGs) | 잡담 | 규칙 |
| [ksf9tm7eyI4](https://www.youtube.com/watch?v=ksf9tm7eyI4) | 기타/기획 | 영상별 검토 |
| [h5gzZkssz-w](https://www.youtube.com/watch?v=h5gzZkssz-w) | 게임 | 규칙 |
| [FOeakXr7j_A](https://www.youtube.com/watch?v=FOeakXr7j_A) | 기타/기획 | 규칙 |
| [ht4M-Mj-lWA](https://www.youtube.com/watch?v=ht4M-Mj-lWA) | 게임 | 규칙 |
| [wsSt8S9wRl4](https://www.youtube.com/watch?v=wsSt8S9wRl4) | 기타/기획 | 영상별 검토 |
| [aUckTmwn8BI](https://www.youtube.com/watch?v=aUckTmwn8BI) | 이벤트 | 규칙 |
| [xc7Y9rIuTB0](https://www.youtube.com/watch?v=xc7Y9rIuTB0) | 기타/기획 | 규칙 |
| [nsn5iMARnQQ](https://www.youtube.com/watch?v=nsn5iMARnQQ) | 기타/기획 | 규칙 |
| [NHxXQ3GYZCc](https://www.youtube.com/watch?v=NHxXQ3GYZCc) | 기타/기획 | 규칙 |
| [sY3oRzO5JtQ](https://www.youtube.com/watch?v=sY3oRzO5JtQ) | 잡담 | 영상별 검토 |
| [Ch_s7aIVHHo](https://www.youtube.com/watch?v=Ch_s7aIVHHo) | 기타/기획 | 규칙 |
| [VEWucL1oGWY](https://www.youtube.com/watch?v=VEWucL1oGWY) | 잡담 | 규칙 |
| [UbE1V8D_lWc](https://www.youtube.com/watch?v=UbE1V8D_lWc) | 게임 | 규칙 |
| [_IY8xsvkPf8](https://www.youtube.com/watch?v=_IY8xsvkPf8) | 잡담 | 영상별 검토 |
| [0ko7LKM3yoU](https://www.youtube.com/watch?v=0ko7LKM3yoU) | 기타/기획 | 규칙 |
| [bovldKh3f50](https://www.youtube.com/watch?v=bovldKh3f50) | 게임 | 규칙 |
| [jUG_Asz9PSc](https://www.youtube.com/watch?v=jUG_Asz9PSc) | 게임 | 규칙 |
| [KVI5yOzbjXg](https://www.youtube.com/watch?v=KVI5yOzbjXg) | 이벤트 | 규칙 |
| [KQZA-5FOgWQ](https://www.youtube.com/watch?v=KQZA-5FOgWQ) | 노래 | 영상별 검토 |
| [GfuKjXV1AY8](https://www.youtube.com/watch?v=GfuKjXV1AY8) | 잡담 | 규칙 |
| [SknkzBIVA9A](https://www.youtube.com/watch?v=SknkzBIVA9A) | 이벤트 | 규칙 |
| [KOs-4rGfSsE](https://www.youtube.com/watch?v=KOs-4rGfSsE) | 게임 | 규칙 |
| [B3oFOpzvocY](https://www.youtube.com/watch?v=B3oFOpzvocY) | 뉴스/정보 | 규칙 |
| [seZvn7zCsAE](https://www.youtube.com/watch?v=seZvn7zCsAE) | 잡담 | 영상별 검토 |
| [AvQlyDZvnh4](https://www.youtube.com/watch?v=AvQlyDZvnh4) | 기타/기획 | 규칙 |
| [Kqc8NNMVXPo](https://www.youtube.com/watch?v=Kqc8NNMVXPo) | 게임 | 규칙 |
| [Z5pC5-L6rt8](https://www.youtube.com/watch?v=Z5pC5-L6rt8) | 노래 | 규칙 |
| [5khIfZ6FmkI](https://www.youtube.com/watch?v=5khIfZ6FmkI) | 멤버십 | 영상별 검토 |
| [ijTWZNQgxg4](https://www.youtube.com/watch?v=ijTWZNQgxg4) | 기타/기획 | 규칙 |
| [RpX4HlK70jk](https://www.youtube.com/watch?v=RpX4HlK70jk) | 기타/기획 | 규칙 |
