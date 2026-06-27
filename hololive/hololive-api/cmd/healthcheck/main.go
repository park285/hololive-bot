package main

import (
	"fmt"
	"os"

	"github.com/park285/shared-go/pkg/healthprobe"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: healthcheck <url> [url...]")
		os.Exit(2)
	}
	for _, target := range os.Args[1:] {
		if err := healthprobe.CheckURLInternal(target); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
