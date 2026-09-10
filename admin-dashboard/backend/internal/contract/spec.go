package contract

import (
	_ "embed"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
)

//go:embed openapi.json
var specJSON []byte

// Spec은 실행 버전이 반영된 OpenAPI 복사본을 반환합니다.
func Spec(version string) map[string]any {
	var spec map[string]any

	if err := jsonv2.Unmarshal(specJSON, &spec); err != nil {
		panic(fmt.Errorf("decode embedded openapi spec: %w", err))
	}

	if info, ok := spec["info"].(map[string]any); ok {
		info["version"] = version
	}

	return spec
}

// MarshalSpec은 실행 버전의 OpenAPI를 결정적 JSON으로 직렬화합니다.
func MarshalSpec(version string) ([]byte, error) {
	out, err := jsonv2.Marshal(Spec(version), jsonv2.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return out, fmt.Errorf("marshal: %w", err)
	}

	return out, nil
}
