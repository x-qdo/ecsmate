package main

import (
	"os"

	"github.com/qdo/ecsmate/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
