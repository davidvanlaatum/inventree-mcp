package tools

import (
	"github.com/davidvanlaatum/dvgoutils"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AnnotationClass struct {
	ReadOnly    bool
	Destructive bool
	Idempotent  bool
	OpenWorld   bool
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
