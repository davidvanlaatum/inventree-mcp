package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptManifestClassifiesMilestoneAndFuturePrompts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	entries := promptManifestByName()

	for _, name := range []string{
		NewPartEntryChecklistPromptName,
		ParameterReuseChecklistPromptName,
		AttachmentImageChecklistPromptName,
		InitialStockEntryChecklistPromptName,
		PurchasePreviewChecklistPromptName,
	} {
		entry, ok := entries[name]
		r.True(ok, "missing prompt %s", name)
		a.Equal(PromptMilestone1, entry.Status, name)
		a.NotEmpty(entry.Checklist, name)
	}

	for _, name := range []string{"receive_purchase_order_checklist", "bom_import_review", "stocktake_review"} {
		entry, ok := entries[name]
		r.True(ok, "missing future prompt %s", name)
		a.Equal(PromptFuture, entry.Status, name)
		a.Empty(entry.Checklist, name)
	}
}

func TestMilestonePromptsPreferClarificationDryRunAndStableIDs(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	for _, entry := range PromptManifest {
		if entry.Status != PromptMilestone1 {
			continue
		}
		text := strings.ToLower(entry.Checklist)
		a.Contains(text, "stable", entry.Name)
		a.Contains(text, "clarification", entry.Name)
		a.NotContains(text, "customer", entry.Name)
		a.NotContains(text, "sales", entry.Name)
	}

	a.Contains(strings.ToLower(promptManifestByName()[NewPartEntryChecklistPromptName].Checklist), "dry_run:true")
	a.Contains(strings.ToLower(promptManifestByName()[InitialStockEntryChecklistPromptName].Checklist), "dry_run:true")
	a.Contains(strings.ToLower(promptManifestByName()[PurchasePreviewChecklistPromptName].Checklist), "no-write")
}

func promptManifestByName() map[string]PromptManifestEntry {
	entries := make(map[string]PromptManifestEntry, len(PromptManifest))
	for _, entry := range PromptManifest {
		entries[entry.Name] = entry
	}
	return entries
}
