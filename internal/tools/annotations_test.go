package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadOnlyAnnotationsSetExplicitPointerFalseHints(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	annotations := ToolAnnotations(ReadOnlyAnnotations)

	r.NotNil(annotations.DestructiveHint)
	r.NotNil(annotations.OpenWorldHint)
	a.True(annotations.ReadOnlyHint)
	a.False(*annotations.DestructiveHint)
	a.True(annotations.IdempotentHint)
	a.False(*annotations.OpenWorldHint)
}

func TestAnnotationJSONMatchesSDKExplicitFalseBehavior(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	data, err := json.Marshal(ToolAnnotations(AnnotationClass{}))

	r.NoError(err)
	a.Contains(string(data), `"destructiveHint":false`)
	a.Contains(string(data), `"idempotentHint":false`)
	a.Contains(string(data), `"openWorldHint":false`)
	a.Contains(string(data), `"readOnlyHint":false`)
}
