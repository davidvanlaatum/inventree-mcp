package tools

import (
	"context"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePartRequirementsClient struct {
	requirements map[int]inventree.PartRequirements
}

func newFakePartRequirementsClient() *fakePartRequirementsClient {
	return &fakePartRequirementsClient{requirements: map[int]inventree.PartRequirements{}}
}

func (f *fakePartRequirementsClient) GetPartRequirements(_ context.Context, id int) (inventree.PartRequirements, error) {
	value, ok := f.requirements[id]
	if !ok {
		return inventree.PartRequirements{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return value, nil
}

func partRequirementsDeps(fake *fakePartRequirementsClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func TestGetPartRequirementsReturnsRecord(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRequirementsClient()
	fake.requirements[5] = inventree.PartRequirements{TotalStock: 10, CanBuild: 5, ScheduledToBuild: 2}
	handler := getPartRequirements(partRequirementsDeps(fake))

	_, output, err := handler(ctx, &mcp.CallToolRequest{}, GetPartRequirementsInput{PartID: 5})
	a.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.InDelta(10, output.Record.TotalStock, 0)
	a.InDelta(5, output.Record.CanBuild, 0)
	a.Equal(2, output.Record.ScheduledToBuild)
}

func TestGetPartRequirementsReturnsNotFoundForMissingRecord(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRequirementsClient()

	_, out, err := getPartRequirements(partRequirementsDeps(fake))(ctx, &mcp.CallToolRequest{}, GetPartRequirementsInput{PartID: 99})
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, out.Status)
}

func TestGetPartRequirementsRejectsNonPositivePartID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartRequirementsClient()

	for _, partID := range []int{0, -1} {
		_, out, err := getPartRequirements(partRequirementsDeps(fake))(ctx, &mcp.CallToolRequest{}, GetPartRequirementsInput{PartID: partID})
		require.NoError(t, err)
		assert.Equal(t, StatusNotFound, out.Status)
	}
}
