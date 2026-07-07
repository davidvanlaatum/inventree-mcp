package tools

import (
	"github.com/davidvanlaatum/dvgoutils"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AnnotationClass struct {
	ReadOnly    bool `json:"read_only"`
	Destructive bool `json:"destructive"`
	Idempotent  bool `json:"idempotent"`
	OpenWorld   bool `json:"open_world"`
}

var ReadOnlyAnnotations = AnnotationClass{
	ReadOnly:    true,
	Destructive: false,
	Idempotent:  true,
	OpenWorld:   false,
}

var WriteAnnotations = AnnotationClass{
	ReadOnly:    false,
	Destructive: false,
	Idempotent:  false,
	OpenWorld:   false,
}

func ToolAnnotations(class AnnotationClass) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    class.ReadOnly,
		DestructiveHint: dvgoutils.Ptr(class.Destructive),
		IdempotentHint:  class.Idempotent,
		OpenWorldHint:   dvgoutils.Ptr(class.OpenWorld),
	}
}
