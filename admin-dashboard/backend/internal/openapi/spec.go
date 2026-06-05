package openapi

import (
	_ "embed"
	stdjson "encoding/json"

	"github.com/park285/shared-go/pkg/json"
)

//go:embed spec.json
var specJSON []byte

func Spec(version string) map[string]any {
	var spec map[string]any
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		return map[string]any{
			"openapi": "3.1.0",
			"info":    map[string]any{"title": "admin-dashboard", "version": version},
			"paths":   map[string]any{},
		}
	}
	if info, ok := spec["info"].(map[string]any); ok {
		info["version"] = version
	}
	return spec
}

func MarshalSpec(version string) ([]byte, error) {
	return stdjson.MarshalIndent(Spec(version), "", "  ")
}
