# mekPark 방송자 모델 실험

DB와 공개 YouTube 제목을 합쳐 문자 TF-IDF + LogisticRegression을 실제 학습하는
오프라인 실험이다. 각 유닛의 세 멤버를 분류하며 원문과 이름·태그 제거 변형을 별도로 학습한다.
서비스 runtime이나 DB에 결과를 쓰지 않는다.

## 입력과 수집

- `hololive/hololive-shared/pkg/domain/mekparkhost/testdata/title_corpus.json`: 현재 DB 제목 회귀
  표본 221건이다. 2026-09-06 실험 당시에는 195건이었으며 9월 11일에 26건을 추가했다.
  제목 근거를 검토한 기대값이며 실제 영상 독립 검수 정답은 아니다.
- `event-times.json`: read-only guard를 증명한 DB의 시작/예약/게시 시각이다.
- `public-titles.json`: UNIT B와 ACHRORA의 videos/streams/shorts에서 직접 읽은 공개 제목
  312건이다. 6개 탭은 모두 마지막 페이지까지 수집했으며 player metadata 조회도 312건 성공했다.
- `review-labels.json`: 모델을 보기 전에 정리한 평가 전용 사례다. 제목·개별 설명 태그에서
  식별한 두 개인 사례, 전체/릴레이 방송, 끝내 확인하지 못한 사례를 구분한다.

공개 수집은 기존 collector-owned YouTube.js 18.0.0을 사용한다. Node 24.20.0과 해당 모듈의
설치된 의존성이 필요하다. 저장소 루트에서 실행한다.

```bash
node scripts/experiments/mekpark-host/fetch-public-titles.mjs
```

이 명령은 `public-titles.json`의 스냅샷을 새로 쓰므로, 과거 결과를 재현할 때는 다시 수집하지 않는다.
로그인·쿠키·운영 helper를 사용하지 않으며 각 탭은 20페이지/1,000건, 개별 요청은 20초로 제한한다.
완료와 부분 수집을 구분하고 부분 수집이면 실패로 종료한다. 설명은 수동 라벨 검토에만 사용하며
학습 입력에 포함하지 않는다. 공개 메타데이터에도 정확한 시각이 없으면 미상으로 남긴다.

## 학습과 재현

```bash
uv run --locked scripts/experiments/mekpark-host/experiment.py
uv run --locked scripts/experiments/mekpark-host/experiment.py --output .tmp/mekpark-host-ml-reproduce
cmp .tmp/mekpark-host-ml/results.json .tmp/mekpark-host-ml-reproduce/results.json
cmp .tmp/mekpark-host-ml/dataset.json .tmp/mekpark-host-ml-reproduce/dataset.json
```

Python 3.14와 scikit-learn 1.9.0을 사용하며 PEP 723 metadata와 `experiment.py.lock`이 실험
환경을 고정한다. Go 기준선은 `mekparkhost/cmd/classify-titles`로 실제 판별기를 호출한다.

9월 6일 보고서는 당시 입력과 규칙으로 고정된 결과다. 현재 규칙·제목 표본으로 실행한 결과와
입력 hash는 달라진다. 신규 표본의 시각을 `event-times.json`이나 공개 metadata에 반영하기
전까지 기존 실험은 정확한 시각이 없는 신규 영상을 시간 분할에서 제외한다.

2026-08-28 00:00 KST를 경계로 과거를 학습, 이후를 holdout으로 둔다. 두 유닛별로 같은
제목 그룹이 fold나 holdout 양쪽에 들어가지 않도록 확인한다. 이름 제거 후 같은 제목도 같은
그룹으로 취급한다. 시각 미상·다중 진행자·정답 미상·별도 검토 사례는 학습에서 제외한다.
clip과 원방송의 영상 ID가 다르고 제목도 다르면 같은 원본 방송으로 묶는 작업까지는 하지 않는다.

C는 0.5/2/8 중 학습 자료의 3-fold StratifiedGroupKFold macro F1로 선택하며 동률이면 작은 C를
사용한다. 어휘·IDF와 분류기 fit은 각 fold의 학습 부분만 본다. 10건 이상을 수락하면서
정밀도 95% 이상인 가장 낮은 점수 기준을 학습 fold 예측에서 선택한다. 후보가 없으면 전부 보류한다.
점수는 별도로 보정한 확률이 아니며 이 기준은 운영 성능 보장이 아니다.

기본 출력은 `.tmp/mekpark-host-ml/` 아래에 생성한다.

- `results.json`: 전체 비교, 학습/평가 ID, fold, 오분류, 미상 제목 예측과 파일 hash.
- `dataset.json`: video_id로 합친 제목과 원래 DB 제목, 시각·출처·라벨 근거.
- `models/*.joblib`: 학습된 유닛별 원문/이름 제거 모델 네 개.
- `models/input-contract.json`: 전처리, 멤버 사전, 환경과 입력 hash.

같은 실행에서 생성한 모델을 다시 읽어 예측의 일치도 확인한다. 저장된 모델로 제목 하나를
확인할 때는 다음처럼 실행한다. 이 명령은 재학습하지 않는다.

```bash
uv run --locked scripts/experiments/mekpark-host/experiment.py \
  --unit unit-b --predict '【ホロドリ】EXフルコン出来ると思ったら？'
```

`predicted_host`는 1순위 후보이며 `accepted`가 false이면 선택한 점수 기준에서 보류한 것이다.
`joblib` 모델은 이 실험에서 직접 생성한 파일만 읽는다.

## 결과의 해석

[2026-09-06 결과](../../../docs/review/mekpark-host-ml-20260906.md)에 실제 실행 결과를 기록했다.
316개 고유 영상 중 221건으로 학습하고 48건을 시간 순서로 분리해 평가했다. 원문 모델은
48/48, 이름·태그·이모지 제거 모델은 31/48이었다. 후자는 인위적으로 입력을 바꾼 시험이며
실제 이름 없는 제목 전체의 정확도를 뜻하지 않는다.

평가용 기대값에 룰에서 만든 약한 라벨이 포함되므로 룰과 원문 모델의 100% 일치가 독립적인
실제 출연자 정확도를 증명하지 않는다. 수동 검토한 룰 보류 사례 두 건은 원문 모델의 후보가
맞았지만 둘 다 자동 수락 기준 아래였다. 유닛 소개 티저를 한 사람으로 추정한 사례도 있어
현재 모델을 보류 항목의 자동 확정에 연결하지 않는다.
