# Source Observation Canonical JSON v1

## 상태와 적용 범위

`source-observation-canonical-json-v1`은 `scope_sha256`, `payload_sha256`, `evidence_sha256`과 `observation_key` 입력을 만드는 canonical JSON bytes 계약이다. source observation contract generation `1`은 이 profile을 사용한다.

이 profile은 RFC 8785 JSON Canonicalization Scheme의 출력 규칙을 따르되 JSON number를 JavaScript safe integer 범위의 정수 값으로 제한한 strict subset이다. 허용된 입력의 출력은 RFC 8785/JCS 출력과 동일하다. profile 규칙을 바꾸려면 fixture와 contract generation을 함께 version-up해야 하며 기존 generation의 hash를 조용히 재해석하면 안 된다.

구현의 기준은 다음 language-neutral fixture다.

```text
hololive/hololive-shared/pkg/contracts/sourceobservation/testdata/canonical_json_v1.json
```

## 입력 계약

- 입력과 canonical 출력은 각각 최대 1 MiB다.
- 입력은 유효한 UTF-8 JSON text여야 하며 하나의 JSON value만 포함한다.
- decoded object member name은 중복될 수 없다. escape spelling이 달라도 decoded name이 같으면 중복이다.
- lone surrogate, 잘못된 UTF-8과 잘못된 JSON escape는 거부한다.
- array/object nesting은 최대 128단계다.
- `null`, boolean, string, array와 object를 허용한다.
- number는 IEEE-754 변환 전 JSON number token의 수학적 값이 정수이고 `-9007199254740991` 이상 `9007199254740991` 이하여야 한다. `1.0`, `1e3`, `1.2300e2`처럼 정수 값을 나타내는 spelling은 허용하지만 소수 값과 범위 밖 정수는 거부한다.
- 구현은 `JSON.parse` 뒤의 binary floating-point 값만으로 number acceptance를 판정하면 안 된다. `9007199254740990.5`처럼 정수로 반올림되거나 `1e-400`처럼 zero로 underflow되는 token도 원래 수학적 값이 소수이므로 거부한다.

fractional provider value가 필요해지면 임의 반올림을 추가하지 않는다. typed schema에서 scaled integer 또는 decimal string을 정의하거나 새 canonical profile과 contract generation을 도입한다.

## 출력 계약

- UTF-8로 인코딩하고 BOM, 줄바꿈과 token 사이 whitespace를 출력하지 않는다.
- `null`, `true`, `false`는 해당 lowercase literal로 출력한다.
- 정수는 leading zero, plus sign, decimal point와 exponent가 없는 최소 base-10 spelling으로 출력한다. negative zero는 `0`이다.
- array element 순서는 입력 의미 그대로 보존하며 element 안의 object는 재귀적으로 canonicalize한다.
- object member는 decoded/unescaped name의 UTF-16 code unit을 unsigned integer로 비교해 재귀적으로 오름차순 정렬한다. locale과 Go map iteration order를 사용하지 않는다.
- string은 Unicode normalization을 수행하지 않는다. U+0008, U+0009, U+000A, U+000C, U+000D는 각각 `\b`, `\t`, `\n`, `\f`, `\r`로 출력하고 나머지 U+0000–U+001F는 lowercase `\u00xx`로 출력한다. quote와 backslash만 각각 `\"`, `\\`로 escape하며 그 밖의 Unicode code point와 `/`, `<`, `>`, `&`, U+2028, U+2029는 그대로 출력한다.

SHA-256은 canonical output bytes 전체에 newline이나 profile prefix를 덧붙이지 않고 계산한다. profile version은 source observation contract generation이 소유한다.

## Fixture conformance

fixture의 `cases`는 input, expected canonical UTF-8 text와 lowercase SHA-256을 제공한다. `rejections`는 모든 구현이 fail closed해야 하는 입력을 제공한다. Go 구현과 향후 다른 언어 구현은 같은 fixture를 수정 없이 읽어 모든 case를 통과해야 한다.

새 fixture case는 최소 다음 경계를 보존해야 한다.

- recursive object ordering과 array order
- JCS string escaping과 raw Unicode
- UTF-16 property ordering
- equivalent safe-integer spellings
- duplicate decoded name, invalid Unicode, fractional/unsafe number, trailing value와 depth overflow 거부

## 언어와 runtime 경계

현재 collector는 Go로 유지한다. TypeScript collector는 언어 선호 때문에 도입하지 않는다. YouTube.js의 8개 kind가 실제 활성화된 뒤 helper RPC call 수·latency·CPU 또는 failure amplification이 material bottleneck이라는 동일 workload evidence가 있을 때만 검토한다. 그 검토 전에 Go와 TypeScript 양쪽이 이 fixture를 통과해야 한다.
