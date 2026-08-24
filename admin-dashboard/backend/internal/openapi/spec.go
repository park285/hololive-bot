package openapi

import (
	_ "embed"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
)

//go:embed spec.json
var specJSON []byte

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

func MarshalSpec(version string) ([]byte, error) {
	out, err := jsonv2.Marshal(Spec(version), jsonv2.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return out, fmt.Errorf("marshal: %w", err)
	}

	return out, nil
}
