package inventreemcp_test

import (
	"strings"
	"testing"

	inventreemcp "github.com/davidvanlaatum/inventree-mcp"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestReadmeCompatTableMatchesBlockingPin(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	const anchor = "<!-- inventree-compat-table:current-row -->"
	lines := strings.Split(inventreemcp.ReadmeMarkdown(), "\n")

	idx := -1
	for i, line := range lines {
		if strings.Contains(line, anchor) {
			idx = i
			break
		}
	}
	r.NotEqual(-1, idx, "README is missing the %q anchor", anchor)
	r.Less(idx+1, len(lines), "README's compat-table anchor has no following row")

	row := lines[idx+1]
	cells := strings.Split(row, "|")
	r.GreaterOrEqual(len(cells), 4, "compat-table current row %q does not have the expected pipe-delimited cells", row)

	r.Equal(testenv.DefaultVersion, strings.TrimSpace(cells[2]),
		"README compat-table current row's InvenTree version does not match the blocking testenv pin")
	r.Equal(testenv.DefaultAPIVersion, strings.TrimSpace(cells[3]),
		"README compat-table current row's API revision does not match the blocking testenv pin")
}
