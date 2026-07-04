package main

import (
	"fmt"
	"io"
	"os"

	"github.com/davidvanlaatum/inventree-mcp/internal/config"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

func run(args []string, stdout, stderr io.Writer, getenv config.Env) int {
	if len(args) == 0 {
		if err := writeLine(stderr, "usage: inventree-mcp serve [flags]"); err != nil {
			return 1
		}
		return 2
	}

	switch args[0] {
	case "serve":
		_, err := config.ParseServeWithEnv(args[1:], getenv, stderr)
		if err != nil {
			if err := writeLine(stderr, "inventree-mcp: %v", err); err != nil {
				return 1
			}
			return 2
		}
		return 0
	case "help", "-h", "--help":
		if err := writeLine(stdout, "usage: inventree-mcp serve [flags]"); err != nil {
			return 1
		}
		return 0
	default:
		if err := writeLine(stderr, "inventree-mcp: unknown command %q", args[0]); err != nil {
			return 1
		}
		return 2
	}
}

func writeLine(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format+"\n", args...)
	return err
}
