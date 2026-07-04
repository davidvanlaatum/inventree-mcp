package main

import (
	"bytes"
	"errors"
	"flag"
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
		var flagOutput bytes.Buffer
		_, err := config.ParseServeWithEnv(args[1:], getenv, &flagOutput)
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				_, _ = io.Copy(stdout, &flagOutput)
				return 0
			}
			_, _ = io.Copy(stderr, &flagOutput)
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
