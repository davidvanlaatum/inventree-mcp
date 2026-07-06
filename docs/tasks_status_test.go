package docs_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskIndexStatusesMatchStoryStatuses(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	data, err := os.ReadFile("TASKS.md")
	r.NoError(err)

	text := string(data)
	indexStatuses := taskIndexStatuses(text)
	r.NotEmpty(indexStatuses)

	storyStatuses := storyStatuses(text)
	for taskID, indexStatus := range indexStatuses {
		storyStatus, ok := storyStatuses[taskID]
		a.True(ok, "missing story section for %s", taskID)
		if ok {
			a.Equal(indexStatus, storyStatus, "status mismatch for %s", taskID)
		}
	}
	for taskID := range storyStatuses {
		_, ok := indexStatuses[taskID]
		a.True(ok, "missing Task Index row for %s", taskID)
	}
}

func taskIndexStatuses(text string) map[string]string {
	statuses := map[string]string{}
	rowPattern := regexp.MustCompile(`(?m)^\| \[([A-Z0-9]+-[A-Z0-9]+)\]\([^)]*\) \| [^|]+ \| ([A-Za-z]+) \|$`)
	for _, match := range rowPattern.FindAllStringSubmatch(text, -1) {
		statuses[match[1]] = match[2]
	}
	return statuses
}

func storyStatuses(text string) map[string]string {
	statuses := map[string]string{}
	sectionPattern := regexp.MustCompile(`(?m)^### ([A-Z0-9]+-[A-Z0-9]+): .*$`)
	matches := sectionPattern.FindAllStringSubmatchIndex(text, -1)
	for i, match := range matches {
		taskID := text[match[2]:match[3]]
		start := match[1]
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		section := text[start:end]
		status := statusLine(section)
		if status != "" {
			statuses[taskID] = status
		}
	}
	return statuses
}

func statusLine(section string) string {
	for _, line := range strings.Split(section, "\n") {
		if status, ok := strings.CutPrefix(line, "- Status: `"); ok {
			return strings.TrimSuffix(status, "`")
		}
	}
	return ""
}
