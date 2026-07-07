package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
)

func main() {
	out := flag.String("out", "", "path to write the generated tool manifest")
	flag.Parse()

	data, err := tools.GenerateToolManifestJSON()
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate tool manifest: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')

	if *out == "" {
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintf(os.Stderr, "write tool manifest: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", *out, err)
		os.Exit(1)
	}
}
