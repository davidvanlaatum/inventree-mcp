package main

import (
	"fmt"
	"io"
	"os"

	"github.com/davidvanlaatum/inventree-mcp/internal/config"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: inventree-mcp serve [flags]")
		return 2
	}

	switch args[0] {
	case "serve":
		_, err := config.ParseServeWithEnv(args[1:], os.Getenv, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "inventree-mcp: %v\n", err)
			return 2
		}
		return 0
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, "usage: inventree-mcp serve [flags]")
		return 0
	default:
		fmt.Fprintf(stderr, "inventree-mcp: unknown command %q\n", args[0])
		return 2
	}
}
