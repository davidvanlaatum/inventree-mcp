package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolsPackageDoesNotImportHTTPEncodingDetails(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	files, err := filepath.Glob("*.go")
	r.NoError(err)
	for _, file := range files {
		data, err := os.ReadFile(file)
		r.NoError(err)
		a.NotContains(string(data), "url."+"Values", file)
	}
}
