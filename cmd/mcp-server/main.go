package main

import (
	"fmt"
	"os"

	"github.com/futuretea/mysql-mcp-server/internal/cmd"
)

func main() {
	command := cmd.NewRootCommand(cmd.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})

	if err := command.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
