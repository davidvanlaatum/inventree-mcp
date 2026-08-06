package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolDescriptorSerializesOfficialInvenTreeIcon(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	descriptor := ToolDescriptor(HealthVersionToolName, "Health and version", "Returns health metadata.")
	r.Len(descriptor.Icons, 1)
	a.Equal(InvenTreeIconSource, descriptor.Icons[0].Source)
	a.Equal("image/png", descriptor.Icons[0].MIMEType)

	data, err := json.Marshal(descriptor)
	r.NoError(err)
	a.JSONEq(`{
		"name": "health_version",
		"title": "Health and version",
		"description": "Returns health metadata.",
		"annotations": {
			"readOnlyHint": true,
			"destructiveHint": false,
			"idempotentHint": true,
			"openWorldHint": false
		},
		"inputSchema": null,
		"icons": [{
			"src": "https://docs.inventree.org/en/latest/assets/logo.png",
			"mimeType": "image/png"
		}]
	}`, string(data))
}

func TestGeneratedToolManifestUsesOfficialInvenTreeIcon(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	manifest := GenerateToolManifest()
	a.Equal(2, manifest.SchemaVersion)
	for _, tool := range manifest.Tools {
		a.Equal(1, len(tool.Icons), tool.Name)
		if len(tool.Icons) == 1 {
			a.Equal(InvenTreeIcon(), tool.Icons[0], tool.Name)
		}
	}
}
