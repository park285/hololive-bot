package main

import (
	"fmt"
	"os"
	"time"

	"github.com/park285/shared-go/pkg/healthprobe"
)

func main() {
	switch {
	case len(os.Args) == 2 && os.Args[1] == "--smoke":
		runSmoke()
	case len(os.Args) == 3 && os.Args[1] == "--body":
		runBody(os.Args[2])
	case len(os.Args) == 2:
		runCheck(os.Args[1])
	default:
		fmt.Fprintln(os.Stderr, "usage: healthcheck <url>|--body <url>|--smoke")
		os.Exit(2)
	}
}

func runBody(url string) {
	body, err := healthprobe.FetchURL(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, _ = os.Stdout.Write(body)
}

func runCheck(url string) {
	if err := healthprobe.CheckURL(url); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runSmoke() {
	for _, name := range []string{"Asia/Seoul", "Asia/Tokyo", "UTC"} {
		if _, err := time.LoadLocation(name); err != nil {
			fmt.Fprintf(os.Stderr, "load location %s: %v\n", name, err)
			os.Exit(1)
		}
	}

	if err := healthprobe.CheckURL("https://www.google.com"); err != nil {
		fmt.Fprintf(os.Stderr, "https ca smoke: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, "smoke ok")
}
