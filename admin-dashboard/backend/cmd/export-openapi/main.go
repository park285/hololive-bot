package main

import (
	"fmt"
	"os"

	"github.com/kapu/admin-dashboard/internal/openapi"
)

func main() {
	payload, err := openapi.MarshalSpec("0.1.0-go")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "export openapi failed: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(append(payload, '\n')); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "write openapi failed: %v\n", err)
		os.Exit(1)
	}
}
