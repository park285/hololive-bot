package main

import (
	"os"

	"github.com/kapu/hololive-shared/pkg/readiness/healthcheckcli"
)

func main() {
	os.Exit(healthcheckcli.Run(os.Args[1:], os.Stderr))
}
