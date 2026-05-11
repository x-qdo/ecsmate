package main

import (
	"fmt"
	"os"

	"github.com/x-qdo/ecsmate/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
}
