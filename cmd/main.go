package main

import (
	"fmt"
	"os"

	"github.com/nareshku/kubectl-container-status/pkg/cmd"
)

func main() {
	if err := cmd.NewContainerStatusCommand().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
