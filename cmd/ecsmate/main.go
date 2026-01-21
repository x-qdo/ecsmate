package main

import (
	"os"

	"github.com/x-qdo/ecsmate/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
