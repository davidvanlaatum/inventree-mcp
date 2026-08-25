package config

import (
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPackagedAndExampleYAMLDecodeCleanly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		path             string
		wantTransport    Transport
		wantInvenTreeURL string
	}{
		{
			name:             "packaged config template",
			path:             "../../packaging/inventree-mcp.yml",
			wantTransport:    TransportHTTP,
			wantInvenTreeURL: "https://inventory.example.test",
		},
		{
			name:             "documented example",
			path:             "../../docs/examples/inventree-mcp.yml",
			wantTransport:    TransportStdio,
			wantInvenTreeURL: "https://inventory.example.test",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			content, err := os.ReadFile(tc.path)
			r.NoError(err, "read %s", tc.path)

			fs := afero.NewMemMapFs()
			r.NoError(afero.WriteFile(fs, "/config.yml", content, 0o600))

			fileCfg, err := loadConfigFile(fs, "/config.yml")
			r.NoError(err, "decode %s through the real config-file loader", tc.path)

			cfg := defaultConfig()
			r.NoError(applyFileConfig(&cfg, fileCfg))

			r.Equal(tc.wantTransport, cfg.Transport)
			r.Equal(tc.wantInvenTreeURL, cfg.InvenTreeURL)
		})
	}
}
