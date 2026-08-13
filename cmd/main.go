package main

import (
	"os"

	"github.com/somaz94/kube-ctx/cmd/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
