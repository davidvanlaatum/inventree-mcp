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
		writeLine(stderr, "usage: inventree-mcp serve [flags]")
		return 2
	}

	switch args[0] {
	case "serve":
		_, err := config.ParseServeWithEnv(args[1:], getenv, stderr)
		if err != nil {
			writeLine(stderr, "inventree-mcp: %v", err)
			return 2
		}
		return 0
	case "help", "-h", "--help":
		writeLine(stdout, "usage: inventree-mcp serve [flags]")
		return 0
	default:
		writeLine(stderr, "inventree-mcp: unknown command %q", args[0])
		return 2
	}
}

func writeLine(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}
