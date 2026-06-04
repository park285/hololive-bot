package main

import (
	"fmt"
	"os"

	"github.com/kapu/admin-dashboard/internal/openapi"
	"github.com/park285/shared-go/pkg/json"
)

func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(openapi.Spec("0.1.0-go")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "export openapi failed: %v\n", err)
		os.Exit(1)
	}
}
