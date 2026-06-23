package main

import (
	"fmt"
	"os"

	"github.com/park285/shared-go/pkg/healthprobe"
)

func main() {
	url := "http://127.0.0.1:30190/health"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}
	if err := healthprobe.CheckURLInternal(url); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
