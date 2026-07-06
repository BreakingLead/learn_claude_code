package main

import (
	"bee_agent/internal/agent"
	"fmt"
	"os"
)

func main() {
	options, err := agent.ParseRunOptions(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	agent.Run(options)
}
