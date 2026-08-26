package packaging_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type goreleaserConfig struct {
	NFPMs []struct {
		Contents []struct {
			Src      string `yaml:"src"`
			Dst      string `yaml:"dst"`
			Type     string `yaml:"type"`
			FileInfo struct {
				Mode  uint32 `yaml:"mode"`
				Owner string `yaml:"owner"`
				Group string `yaml:"group"`
			} `yaml:"file_info"`
		} `yaml:"contents"`
		Scripts struct {
			PreInstall  string `yaml:"preinstall"`
			PostInstall string `yaml:"postinstall"`
			PreRemove   string `yaml:"preremove"`
			PostRemove  string `yaml:"postremove"`
		} `yaml:"scripts"`
	} `yaml:"nfpms"`
}

func TestPackagedNFPMContentsUseConfigFile(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	raw, err := os.ReadFile("../.goreleaser.yaml")
	r.NoError(err)

	var cfg goreleaserConfig
	r.NoError(yaml.Unmarshal(raw, &cfg))
	r.Len(cfg.NFPMs, 1)

	var foundConfig bool
	for _, entry := range cfg.NFPMs[0].Contents {
		a.False(strings.Contains(entry.Src, "inventree-mcp.env") || strings.Contains(entry.Dst, "inventree-mcp.env"),
			"nfpm contents must not reference the retired inventree-mcp.env template: %+v", entry)
		if entry.Dst == "/etc/inventree-mcp/config.yml" {
			foundConfig = true
			a.Equal("packaging/inventree-mcp.yml", entry.Src)
			a.Equal("config|noreplace", entry.Type)
			a.Equal(uint32(0o600), entry.FileInfo.Mode)
			a.Equal("inventree-mcp", entry.FileInfo.Owner)
			a.Equal("inventree-mcp", entry.FileInfo.Group)
		}
	}
	a.True(foundConfig, "expected an nfpm contents entry installing /etc/inventree-mcp/config.yml")
}

func TestPackagedNFPMScriptsCreateNonRootIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	raw, err := os.ReadFile("../.goreleaser.yaml")
	r.NoError(err)

	var cfg goreleaserConfig
	r.NoError(yaml.Unmarshal(raw, &cfg))
	r.Len(cfg.NFPMs, 1)

	a.Equal("packaging/scripts/preinstall.sh", cfg.NFPMs[0].Scripts.PreInstall)
	a.Equal("packaging/scripts/postinstall.sh", cfg.NFPMs[0].Scripts.PostInstall)
	a.Equal("packaging/scripts/preremove.sh", cfg.NFPMs[0].Scripts.PreRemove)
	a.Equal("packaging/scripts/postremove.sh", cfg.NFPMs[0].Scripts.PostRemove)
}
