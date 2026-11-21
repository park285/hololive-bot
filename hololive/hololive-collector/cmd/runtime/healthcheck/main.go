package main

import (
	"os"

	"github.com/park285/shared-go/transport/healthprobe"
)

func main() {
	os.Exit(healthprobe.RunMain(os.Args, os.Stdout, os.Stderr))
}
