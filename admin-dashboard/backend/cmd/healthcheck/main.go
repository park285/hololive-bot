package main

import (
	"os"

	"github.com/park285/shared-go/pkg/healthprobe"
)

func main() {
	args := os.Args
	if len(args) == 1 {
		args = append(args, "http://127.0.0.1:30190/health")
	}
	os.Exit(healthprobe.RunMain(args, os.Stdout, os.Stderr))
}
