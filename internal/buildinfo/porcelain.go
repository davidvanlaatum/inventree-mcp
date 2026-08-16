package buildinfo

import (
	"fmt"
	"strings"
)

// PorcelainMarker is the current schema version of the `version` command's key:value
// output. Only an incompatible change to an existing key's meaning requires bumping it;
// a later revision may add keys without doing so.
const PorcelainMarker = "1"

const (
	keyPorcelain        = "porcelain"
	keyVersion          = "version"
	keyCommit           = "commit"
	keyDate             = "date"
	keyInvenTreeVersion = "inventree_version"
	keyInvenTreeAPI     = "inventree_api"
)

// PorcelainInfo is the parsed form of the `version` command's stable key:value output.
// InvenTreeVersion and InvenTreeAPI are optional: they are empty when the reporting
// build predates internal/buildinfo's InvenTree-baseline constants.
type PorcelainInfo struct {
	Version          string
	Commit           string
	Date             string
	InvenTreeVersion string
	InvenTreeAPI     string
}

// PorcelainLines renders this build's identity as the porcelain-style `version` command
// output: stable `key: value` lines led by an explicit `porcelain: 1` marker.
func PorcelainLines() []string {
	lines := []string{
		keyPorcelain + ": " + PorcelainMarker,
		keyVersion + ": " + Version,
		keyCommit + ": " + Commit,
		keyDate + ": " + Date,
	}
	if PinnedInvenTreeVersion != "" {
		lines = append(lines, keyInvenTreeVersion+": "+PinnedInvenTreeVersion)
	}
	if PinnedInvenTreeAPIVersion != "" {
		lines = append(lines, keyInvenTreeAPI+": "+PinnedInvenTreeAPIVersion)
	}
	return lines
}

// ParsePorcelain parses one `version` command's stdout. Each line is split on the first
// ": " occurrence rather than a fixed line count or a naive split on every colon, since
// `date` is RFC3339 and itself contains colons. Unknown keys are tolerated so a later
// revision can add fields without breaking older parsers. It fails closed when the
// `porcelain` marker is missing or is a version this parser does not understand, or when
// a required key (`version`, `commit`, `date`) is missing.
func ParsePorcelain(output string) (PorcelainInfo, error) {
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			return PorcelainInfo{}, fmt.Errorf("malformed version output line %q", line)
		}
		fields[key] = value
	}
	if marker, ok := fields[keyPorcelain]; !ok || marker != PorcelainMarker {
		return PorcelainInfo{}, fmt.Errorf("unsupported porcelain marker %q", fields[keyPorcelain])
	}
	info := PorcelainInfo{
		Version:          fields[keyVersion],
		Commit:           fields[keyCommit],
		Date:             fields[keyDate],
		InvenTreeVersion: fields[keyInvenTreeVersion],
		InvenTreeAPI:     fields[keyInvenTreeAPI],
	}
	if info.Version == "" || info.Commit == "" || info.Date == "" {
		return PorcelainInfo{}, fmt.Errorf("version output missing required version, commit, or date field")
	}
	return info, nil
}
