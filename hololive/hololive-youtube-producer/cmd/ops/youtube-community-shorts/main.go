package main

import (
	"os"

	"github.com/kapu/hololive-youtube-producer/cmd/ops/internal/communityshortscli"
)

func main() {
	os.Exit(communityshortscli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
