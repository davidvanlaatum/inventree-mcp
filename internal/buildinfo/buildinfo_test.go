package buildinfo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserAgentUsesBuildVersion(t *testing.T) {
	previousVersion := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = previousVersion })

	require.New(t).Equal("inventree-mcp/1.2.3", UserAgent())
}

func TestPorcelainLinesIncludesPinnedInvenTreeBaseline(t *testing.T) {
	a := assert.New(t)
	previousVersion, previousCommit, previousDate := Version, Commit, Date
	Version, Commit, Date = "1.2.3", "abc123", "2026-08-16T10:00:00Z"
	t.Cleanup(func() { Version, Commit, Date = previousVersion, previousCommit, previousDate })

	lines := PorcelainLines()

	a.Equal([]string{
		"porcelain: 1",
		"version: 1.2.3",
		"commit: abc123",
		"date: 2026-08-16T10:00:00Z",
		"inventree_version: " + PinnedInvenTreeVersion,
		"inventree_api: " + PinnedInvenTreeAPIVersion,
	}, lines)
}

func TestParsePorcelainRoundTripsRenderedLines(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	info, err := ParsePorcelain(strings.Join(PorcelainLines(), "\n") + "\n")
	r.NoError(err)
	a.Equal(Version, info.Version)
	a.Equal(Commit, info.Commit)
	a.Equal(Date, info.Date)
	a.Equal(PinnedInvenTreeVersion, info.InvenTreeVersion)
	a.Equal(PinnedInvenTreeAPIVersion, info.InvenTreeAPI)
}

func TestParsePorcelainSplitsOnFirstColonSpaceOnly(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	info, err := ParsePorcelain("porcelain: 1\nversion: 1.2.3\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n")
	r.NoError(err)
	a.Equal("2026-08-16T10:00:00Z", info.Date)
}

func TestParsePorcelainAcceptsMissingOptionalInvenTreeFields(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	info, err := ParsePorcelain("porcelain: 1\nversion: 1.2.3\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n")
	r.NoError(err)
	a.Empty(info.InvenTreeVersion)
	a.Empty(info.InvenTreeAPI)
}

func TestParsePorcelainToleratesUnknownAdditionalKeys(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	info, err := ParsePorcelain("porcelain: 1\nversion: 1.2.3\ncommit: abc\ndate: 2026-08-16T10:00:00Z\nfuture_key: some value\n")
	r.NoError(err)
	a.Equal("1.2.3", info.Version)
}

func TestParsePorcelainRejectsUnsupportedOrMissingMarker(t *testing.T) {
	a := assert.New(t)

	tests := map[string]string{
		"wrong marker version": "porcelain: 2\nversion: 1.2.3\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n",
		"missing marker":       "version: 1.2.3\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n",
	}
	for name, output := range tests {
		_, err := ParsePorcelain(output)
		a.Error(err, name)
	}
}

func TestParsePorcelainRejectsMissingRequiredFields(t *testing.T) {
	a := assert.New(t)

	tests := map[string]string{
		"missing version": "porcelain: 1\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n",
		"missing commit":  "porcelain: 1\nversion: 1.2.3\ndate: 2026-08-16T10:00:00Z\n",
		"missing date":    "porcelain: 1\nversion: 1.2.3\ncommit: abc\n",
	}
	for name, output := range tests {
		_, err := ParsePorcelain(output)
		a.Error(err, name)
	}
}

func TestParsePorcelainSkipsBlankLines(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	info, err := ParsePorcelain("porcelain: 1\nversion: 1.2.3\n\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n")
	r.NoError(err)
	a.Equal("1.2.3", info.Version)
}

func TestParsePorcelainRejectsLineWithoutColonSpaceSeparator(t *testing.T) {
	a := assert.New(t)

	_, err := ParsePorcelain("porcelain: 1\nversion:1.2.3\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n")
	a.ErrorContains(err, "malformed version output line")
}
