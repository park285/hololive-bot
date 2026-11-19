# 관측 JSON 객체 키의 정규 순서

임의 길이의 유효한 두 키를 비교한 결과는 그 키를 UTF-16 코드 단위로 인코딩한 뒤 사전식으로 비교한 결과와 같다. 이 순서는 엄격한 전순서이므로 동일한 키·값 쌍들의 순열 중 키가 엄격히 증가하는 멤버열은 유일하다. 순서가 인코더마다 달라지면 같은 관측의 정규 바이트와 SHA-256이 달라져 검증 또는 중복 판정이 어긋날 수 있다.

## 대상과 대응

대상: `hololive/hololive-shared/pkg/contracts/sourceobservation/canonical_json.go`의 `compareJSONNamesUTF16`, `utf16CodeUnitOrder`와 `sortObjectMembers`가 요구하는 순서 계약.

| Go 연산 | `JSONNameOrder.lean`의 대응 |
| --- | --- |
| `appendMember`가 보관한 디코딩된 이름의 바이트 구간 | `Name`은 유효한 유니코드 스칼라의 목록이다. 입력 표현의 escape 차이 및 UTF-8 바이트 길이는 이름의 의미에 포함하지 않는다. |
| 양쪽에서 `utf8.DecodeRune`를 한 번씩 호출 | `compareJSONNamesUTF16`의 재귀 한 단계가 두 목록 머리를 비교한다. 같을 때에만 양쪽에서 한 스칼라씩 소비한다. |
| 서로 다른 룬의 `utf16CodeUnitOrder` 비교 | U+E000..U+FFFF만 U+110000만큼 뒤로 옮긴다. 다른 룬은 그대로다. `rankInjective`는 이 변환이 서로 다른 키 머리를 합치지 않음을 보인다. |
| 한쪽이 비었을 때 `cmp.Compare(len(left), len(right))` | 남은 양쪽이 비었으면 동등, 한쪽만 비었으면 그쪽이 더 작다. 유효 UTF-8의 비어 있지 않은 접미사는 항상 한 바이트 이상이다. |
| 명세와 정렬 | `prependUnits`와 `utf16`은 별도로 표준 UTF-16의 단일 단위 또는 상·하위 서로게이트 쌍을 만든다. `agreesWithUTF16`은 전체 비교 결과의 일치를, `equalExactly`는 동등이 같은 이름일 때뿐임을 보인다. `irreflexive`, `transitive`, `trichotomy`로 전순서를 얻는다. `canonicalMembersUnique`는 두 정렬 결과가 같은 멤버의 순열이면 값까지 포함해 동일함을 보인다. |

## 가정과 제외 범위

- 길이, 키 개수, 공통 접두사 길이에 탐색 상한이 없다. 실제 디코더가 유효 UTF-8 및 스칼라 범위를 보장하고 중복 이름을 거부한다고 가정한다. 정렬의 유일성은 같은 멤버를 보존하면서 키를 엄격히 증가하도록 정렬한 결과에 대한 조건부 정리이다.
- `slices.SortFunc` 구현, 멤버 바이트 구간 복사, 숫자·문자열의 정규 출력 전체, 중첩과 크기 제한, SHA 계산, JSON 디코더 자체는 증명하지 않는다.
- `hololive/hololive-shared/pkg/contracts/sourceobservation/canonical_name_order_fuzz_test.go`는 실제 비교기를 `unicode/utf16.Encode`와 `slices.Compare`로 대조한다. 서로 다른 두 키의 유효 크기 객체는 각 키에 다른 값을 붙여 두 멤버 순열 모두를 `CanonicalizeJSON`에 넣고 명세의 정렬 바이트와 대조한다.

## 반증 실험

저장소 밖 복사본에서 U+E000..U+FFFF의 순위 `c.val + 0x110000`을 상수 `0xE000`으로 바꾸었다. 같은 `lake build`가 `rankInjective`와 `rankLtUnitsLt` 모두에서 종료 코드 **1**로 실패했다. 변형에서는 U+E000과 U+E001이 동률이며 U+E000을 U+10000보다 앞에 놓는다. 실제 UTF-16에서는 U+10000의 첫 단위 D800이 E000보다 작다. 원본 빌드는 종료 코드 **0**이었다.

## 실행

`hololive-bot` 저장소 루트에서 실행한다.

```sh
(cd verification/lean/JSONNameOrder && bash /home/kapu/work/iris-stack/tools/lint/verify-tools.sh lake build)
export GOWORK="$PWD/go.work"
cd hololive/hololive-shared
go test -run '^FuzzCanonicalNameUTF16Order$' ./pkg/contracts/sourceobservation
go test -run '^$' -fuzz '^FuzzCanonicalNameUTF16Order$' -fuzztime=5s -parallel=2 ./pkg/contracts/sourceobservation
```

동기화 커밋: e85f952921e60e2a0f2f183d1e8e65c081f0ffb5
