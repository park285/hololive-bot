# mekPark 방송자 분류 모델 학습과 비교

**Decisions:** `DEC-20260906-hololive-mekpark-host-ml-evaluation` (governing), `DEC-20260906-hololive-mekpark-title-host-attribution` (context)

## Execution capsule
**Goal:** DB와 두 공유 채널의 공개 YouTube 제목으로 방송자 모델을 학습하고 기존 룰과 비교한다.
**Context:** DB 제목 195건의 기존 기대값은 약한 라벨이다. 영상 55건의 DB 게시 시각은 비어 있어 공개 메타데이터를 보완한다.
**Constraints:** 운영 코드·DB·배포를 바꾸지 않는다. 평가용 Python 의존성만 격리하고, 미상 정답을 추측으로 채우거나 평가 자료로 모델을 선택하지 않는다.
**Evidence:** 기존 mekparkhost 판별기·표본, 읽기 전용 DB 시각, collector YouTube.js의 공개 목록 조회, scikit-learn 1.9 API를 확인했다.
**Success:** 학습된 모델, 분리된 평가 결과·오분류·보류 사례·재현 명령을 제공하고 실제 검수 정확도와 약한 라벨 재현율을 구분한다.
**Output:** scripts/experiments/mekpark-host의 실험 코드·입력·잠금 파일, 로컬 모델 파일, docs/review의 결과 기록을 제공한다.

## 실행 항목

실행 순서는 사용자 추가 요청에 따라 T04 → T01 → T02 → T03이다.

### T01 표본과 평가 경계 고정

기존 corpus와 baseline Go 테스트를 근거로 룰 출력을 고정하고 T04의 공개 제목을 video_id로 합친다. DB에서 두 채널의 시작/예약/게시 시각만 제한 조회하며 `default_transaction_read_only=on`과 `transaction_read_only=on`을 증명한다. 공개 메타데이터의 정확한 시각을 우선 보완하되 상대 시각은 정확한 시각으로 변환하지 않는다. 정확한 시각이 없는 자료는 시간 분할 학습에서 제외한다. 단일 진행자 라벨이 있는 자료를 대상으로 2026-08-28 00:00 KST 이전을 학습, 이후를 holdout으로 둔다. 정규화한 동일 제목은 한 그룹으로 묶고 경계를 가로지르면 과거 중복을 학습에서 제거한다. 합방·정답 미상·단체/비방송은 단일 분류 정답으로 강제하지 않는다. 룰이 보류한 제목의 검토 기록은 학습에 사용하지 않는다. AC01과 V01을 충족한다.

### T02 텍스트 모델 학습과 측정

T01 이후 유닛별 문자 2~5-gram TF-IDF와 LogisticRegression을 학습한다. 원문과 이름·개인 태그·시리즈 신호를 제거한 두 변형을 동일한 분할로 비교한다. C 후보 0.5/2/8은 학습 자료 안의 3-fold StratifiedGroupKFold로 선택한다. 보류 점수 기준도 학습 fold 예측만 사용해 정한다. holdout에는 룰·유닛별 다수 클래스·모델의 정확도/정답 수, macro F1, 오분류와 조건부 정밀도·coverage를 기록한다. 익명화 평가는 스트레스 시험으로 표시한다. 모델 점수는 보정된 확률로 주장하지 않는다. 모델 파일을 로컬에 보존한다. AC02와 V02를 충족한다.

### T03 결과 해석과 검증 기록

T02 이후 원문 평가와 익명화 평가, 제목 검토의 약한 라벨과 정답 미상을 구분하여 보고한다. 기존 룰이 보류한 실제 제목에 대한 예측도 남기되 정답을 모르면 정확도 계산에서 제외한다. 전체 모델과 입력/규칙/코드 hash, 학습·평가 ID, 재현 명령을 보존한다. 같은 입력과 환경에서 두 번째 실행 결과가 일치하는지 확인한다. 모델의 운영 도입 여부는 실험 결과에 근거해 제안만 한다. AC03과 V03을 충족한다.

### T04 공개 YouTube 제목 수집

UNIT B와 ACHRORA의 videos/streams/shorts 공개 목록을 기존 collector-owned YouTube.js로 읽는다. 로그인·쿠키·비밀·운영 helper·DB 쓰기를 사용하지 않는다. 탭마다 최대 20페이지/1,000개와 요청별 timeout을 적용하고 완료/부분 수집/실패를 구분한다. 영상 제목·ID·채널·출처 URL·조회 시각을 저장하고, 공개 player metadata에서 시작·게시 시각을 제한적으로 보완한다. 정확한 시각이나 출연자를 확인하지 못한 경우 미상으로 보존한다. AC04와 V04를 충족한다.

## 수용 기준

### AC01 누출과 라벨 출처 구분

같은 video_id와 정규화 제목 그룹이 학습·holdout 또는 fold 양쪽에 들어가지 않는다. 어휘·IDF·분류기 fit은 해당 학습 fold만 사용한다. 시각 미상, 다중 진행자, 정답 미상을 명시적으로 구분하고 검사한다. 제목 기대값은 실제 영상 독립 검수 정답으로 표기하지 않는다.

### AC02 학습된 모델과 비교 결과

두 유닛 각각 원문/익명화 모델을 학습하고 보존한다. 정확도·macro F1·보류 기준과 판별 범위, 실제 오분류 및 미상 제목의 예측이 결과 JSON에 포함된다. 보류 기준을 만족하는 후보가 없으면 판별을 보류하는 결과로 기록한다.

### AC03 재현 가능성과 운영 경계

uv로 Python을 실행하며 scikit-learn 버전과 전이 의존성을 실험용 잠금 파일로 고정한다. 모델 파일은 로컬 작업 디렉터리 안에만 생성하고 서비스 runtime dependency, DB 쓰기, 실제 발송·배포·Git ref 변경을 하지 않는다. 재실행 결과와 입력/코드 hash를 확인한다.

### AC04 공개 자료 출처와 중복 제거

실험 입력에 두 공유 채널의 공개 YouTube 제목이 포함된다. DB 중복은 video_id로 제거하고 제목 변경 전후와 출처를 보존한다. 수집한 탭의 페이지 수·완료 여부와 시각 미상 수를 기록한다. 공개 제목으로 만든 라벨도 실제 영상 독립 검수 정답과 구분한다.

## 검증

### V01 기준선과 입력 감사

`go test ./hololive/hololive-shared/pkg/domain/mekparkhost -run '^TestIdentifyCorpus$'`를 실행한다. 실험 실행 시 ID 중복, 시각·라벨 누락, 그룹 경계와 fold 중복을 검사하고 입력 감사 결과를 기록한다.

### V02 학습 실행과 재현

`uv run --locked scripts/experiments/mekpark-host/experiment.py`로 학습과 평가를 수행한다. 동일 입력으로 재실행하고 결과 JSON 및 모델 예측의 일치를 확인한다. 각 클래스가 학습 fold에 존재하는지 검증하며 수렴 실패를 경고만 남긴 성공으로 처리하지 않는다.

### V03 산출물과 문서 확인

uv의 Python parser/컴파일 검사, JSON parser 검사, `git diff --check`와 `bash ../tools/checks/check-decision-catalog.sh check --submodules`를 수행한다. 모델·결과·라벨 출처와 최종 diff를 검토하고 제약 및 실험 한계를 기록한다.

### V04 공개 수집 검증

`node --check scripts/experiments/mekpark-host/fetch-public-titles.mjs`를 실행하고 한 번의 제한 수집 결과에서 채널 whitelist, video_id 유효성·중복, 출처 URL, pagination 종료 상태와 정확한 시각을 검사한다.
