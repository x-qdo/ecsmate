package main

import (
	"fmt"
	"os"

	"github.com/x-qdo/ecsmate/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
