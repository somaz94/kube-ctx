package main

import (
	"os"

	"github.com/somaz94/kube-ctx/cmd/cli"
)

func main() {
	// The exit status matters: "kctx exec <ctx> -- kubectl ..." passes the
	// command's own status through, and "kctx doctor" is meant to gate scripts.
	os.Exit(cli.ExitCode(cli.Execute()))
}
