package tools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatePartCategoryRefusesLaterPageNormalizedDuplicate(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryAdminFake()
	fake.pages[0] = inventree.CategoryPage{Count: 101, Results: []inventree.Category{{PK: 1, Name: "Other"}}, HasMore: true}
	fake.pages[100] = inventree.CategoryPage{Count: 101, Results: []inventree.Category{{PK: 2, Name: " pAsSiVeS "}}}

	_, out, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "  Passives  "})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Clarification)
	a.Equal("category_id", out.Clarification.Retry)
	a.Zero(fake.createCalls)
	a.Equal([]int{0, 100}, fake.pageOffsets)
}

func TestCreatePartCategoryFailsClosedAtDuplicateScanBound(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryAdminFake()
	fake.pages[0] = inventree.CategoryPage{Count: categoryAdminScanLimit + 1}

	_, out, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "Bounded"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Contains(out.Clarification.Reason, "safety limit")
	a.Zero(fake.createCalls)
}

func TestCategoryAdministrationClarifiesInvalidInputsBeforeWriting(t *testing.T) {
	t.Parallel()
	t.Run("blank create name", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		_, out, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "  "})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
	})
	t.Run("nonpositive create parent", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		_, out, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "Child", ParentID: dvgoutils.Ptr(0)})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
	})
	t.Run("nonpositive create location", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		_, out, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "Root", DefaultLocationID: dvgoutils.Ptr(0)})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
	})
	t.Run("nonpositive update id", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
	})
	t.Run("conflicting clear", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, ParentID: dvgoutils.Ptr(1), ClearParent: true})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
	})
	t.Run("empty patch", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		fake.categories[10] = inventree.Category{PK: 10, Name: "Existing"}
		_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
	})
	t.Run("blank update name", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		fake.categories[10] = inventree.Category{PK: 10, Name: "Existing"}
		_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, Name: dvgoutils.Ptr(" ")})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
	})
	t.Run("nonpositive update parent", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		fake.categories[10] = inventree.Category{PK: 10, Name: "Existing"}
		_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, ParentID: dvgoutils.Ptr(0)})
		require.NoError(t, err)
		assert.Equal(t, StatusClarificationRequired, out.Status)
	})
}

func TestCreatePartCategoryAllowsSameNameUnderDifferentParentAndReadsBack(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryAdminFake()
	fake.categories[20] = inventree.Category{PK: 20, Name: "Parent"}
	fake.createResult = inventree.Category{PK: 30, Name: "Passives", Parent: dvgoutils.Ptr(20)}

	_, out, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: " Passives ", ParentID: dvgoutils.Ptr(20), Structural: dvgoutils.Ptr(false)})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	r.NotNil(out.Record)
	a.Equal(30, out.Record.PK)
	a.Equal("Passives", fake.lastCreate.Name)
	a.Equal(20, *fake.lastCreate.Parent)
	a.Equal(false, *fake.lastCreate.Structural)
}

func TestUpdatePartCategoryRequiresConfirmationButAllowsPartsAndChildren(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryAdminFake()
	fake.categories[10] = inventree.Category{PK: 10, Name: "Existing", Parent: dvgoutils.Ptr(1), Subcategories: dvgoutils.Ptr(2)}
	fake.categories[20] = inventree.Category{PK: 20, Name: "New parent"}
	fake.partCount = 3
	fake.updateResult = inventree.Category{PK: 10, Name: "Existing", Parent: dvgoutils.Ptr(20), Subcategories: dvgoutils.Ptr(2), PartCount: dvgoutils.Ptr(3)}
	input := UpdatePartCategoryInput{ID: 10, ParentID: dvgoutils.Ptr(20)}

	_, preview, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, preview.Status)
	r.NotNil(preview.Hierarchy)
	a.Equal(3, preview.Hierarchy.DirectPartCount)
	a.Equal(2, preview.Hierarchy.DirectChildCount)
	a.Zero(fake.updateCalls)

	input.Confirm = true
	_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal(1, fake.updateCalls)
	a.Equal(20, *out.Record.Parent)
}

func TestUpdatePartCategoryRefusesDescendantCycle(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryAdminFake()
	fake.categories[10] = inventree.Category{PK: 10, Name: "Parent"}
	fake.categories[11] = inventree.Category{PK: 11, Name: "Child", Parent: dvgoutils.Ptr(10)}

	_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, ParentID: dvgoutils.Ptr(11), Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Clarification)
	a.Contains(out.Clarification.Reason, "descendants")
	a.Zero(fake.updateCalls)
}

func TestUpdatePartCategoryRefusesStructuralPromotionWithDirectParts(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryAdminFake()
	fake.categories[10] = inventree.Category{PK: 10, Name: "Parts", Structural: false}
	fake.partCount = 1

	_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, Structural: dvgoutils.Ptr(true), Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Contains(out.Clarification.Reason, "directly assigned parts")
	a.Zero(fake.updateCalls)
}

func TestUpdatePartCategoryPreservesExplicitClearFalseAndEmpty(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryAdminFake()
	fake.categories[10] = inventree.Category{PK: 10, Name: "Parts", Parent: dvgoutils.Ptr(1), DefaultLocation: dvgoutils.Ptr(40), DefaultKeywords: dvgoutils.Ptr("old"), Icon: dvgoutils.Ptr("icon"), Structural: false}
	fake.updateResult = inventree.Category{PK: 10, Name: "Parts", Description: "", DefaultKeywords: dvgoutils.Ptr(""), Structural: false}

	_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, Description: dvgoutils.Ptr(""), ClearParent: true, ClearDefaultLocation: true, DefaultKeywords: dvgoutils.Ptr(""), Structural: dvgoutils.Ptr(false), ClearIcon: true, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	payload, err := fake.lastPatch.MarshalJSON()
	r.NoError(err)
	a.JSONEq(`{"description":"","parent":null,"default_location":null,"default_keywords":"","structural":false,"icon":null}`, string(payload))
}

func TestCreatePartCategoryRecoversPostPersistResponseLoss(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryAdminFake()
	fake.createErr = errors.New("response lost after persist")
	fake.recoveryCategory = inventree.Category{PK: 77, Name: "Recovered"}

	_, out, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "Recovered"})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	a.True(out.Recovered)
	r.NotNil(out.Record)
	a.Equal(77, out.Record.PK)
	a.Contains(out.RecoveryPlan, "do not retry")
}

func TestCreatePartCategoryTransportResponseLossRecoversPersistedRecord(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	persisted := false
	transport := categoryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/api/part/category/":
			persisted = true
			return nil, errors.New("response lost after persist")
		case req.Method == http.MethodGet && req.URL.Path == "/api/part/category/":
			if persisted {
				return categoryJSONResponse(req, http.StatusOK, `{"count":1,"next":null,"previous":null,"results":[{"pk":77,"name":"Recovered","description":"transport"}]}`), nil
			}
			return categoryJSONResponse(req, http.StatusOK, `{"count":0,"next":null,"previous":null,"results":[]}`), nil
		case req.Method == http.MethodGet && req.URL.Path == "/api/part/category/77/":
			return categoryJSONResponse(req, http.StatusOK, `{"pk":77,"name":"Recovered","description":"transport"}`), nil
		default:
			return categoryJSONResponse(req, http.StatusNotFound, `{}`), nil
		}
	})
	client := newCategoryTransportClient(t, transport)

	_, out, err := createPartCategory(categoryClientDeps(client))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "Recovered", Description: dvgoutils.Ptr("transport")})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	a.True(out.Recovered)
	r.NotNil(out.Record)
	a.Equal(77, out.Record.PK)
}

func TestUpdatePartCategoryTransportResponseLossRecoversExactPatch(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	persisted := false
	transport := categoryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodPatch && req.URL.Path == "/api/part/category/10/":
			persisted = true
			return nil, errors.New("response lost after persist")
		case req.Method == http.MethodGet && req.URL.Path == "/api/part/category/10/":
			description := "before"
			if persisted {
				description = "after"
			}
			return categoryJSONResponse(req, http.StatusOK, `{"pk":10,"name":"Parts","description":"`+description+`"}`), nil
		case req.Method == http.MethodGet && req.URL.Path == "/api/part/":
			return categoryJSONResponse(req, http.StatusOK, `{"count":0,"next":null,"previous":null,"results":[]}`), nil
		case req.Method == http.MethodGet && req.URL.Path == "/api/part/category/":
			return categoryJSONResponse(req, http.StatusOK, `{"count":0,"next":null,"previous":null,"results":[]}`), nil
		default:
			return categoryJSONResponse(req, http.StatusNotFound, `{}`), nil
		}
	})
	client := newCategoryTransportClient(t, transport)

	_, out, err := updatePartCategory(categoryClientDeps(client))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, Description: dvgoutils.Ptr("after")})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	a.True(out.Recovered)
	r.NotNil(out.Record)
	a.Equal("after", out.Record.Description)
}

func TestCreatePartCategoryRecoveryRequiresEverySuppliedField(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryAdminFake()
	fake.createErr = errors.New("response lost after persist")
	fake.recoveryCategory = inventree.Category{PK: 77, Name: "Recovered", Description: "different"}

	_, out, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "Recovered", Description: dvgoutils.Ptr("requested")})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	a.False(out.Recovered)
	a.Contains(out.RecoveryPlan, "does not match every supplied field")
}

func TestUpdatePartCategoryRejectsMismatchedResponseIdentity(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryAdminFake()
	fake.categories[10] = inventree.Category{PK: 10, Name: "Parts"}
	fake.updateResult = inventree.Category{PK: 99, Name: "Parts", Description: "after"}

	_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, Description: dvgoutils.Ptr("after")})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	a.Contains(out.RecoveryPlan, "mismatched category_id 99")
}

func TestCategoryMutationReadBackRejectsMismatchedIdentity(t *testing.T) {
	t.Parallel()
	t.Run("create success", func(t *testing.T) {
		t.Parallel()
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		fake.createResult = inventree.Category{PK: 30, Name: "Created"}
		fake.getAfterMutation[30] = inventree.Category{PK: 31, Name: "Created"}
		_, out, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "Created"})
		require.NoError(t, err)
		assert.Equal(t, StatusPartialFailure, out.Status)
		assert.Contains(t, out.RecoveryPlan, "different identity")
	})
	t.Run("create recovery", func(t *testing.T) {
		t.Parallel()
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		fake.createErr = errors.New("response lost")
		fake.recoveryCategory = inventree.Category{PK: 77, Name: "Created"}
		fake.getAfterMutation[77] = inventree.Category{PK: 78, Name: "Created"}
		_, out, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "Created"})
		require.NoError(t, err)
		assert.False(t, out.Recovered)
		assert.Nil(t, out.Record)
		assert.Contains(t, out.RecoveryPlan, "category_id 77")
	})
	t.Run("update success", func(t *testing.T) {
		t.Parallel()
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		fake.categories[10] = inventree.Category{PK: 10, Name: "Parts"}
		fake.updateResult = inventree.Category{PK: 10, Name: "Parts", Description: "after"}
		fake.getAfterMutation[10] = inventree.Category{PK: 99, Name: "Parts", Description: "after"}
		_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, Description: dvgoutils.Ptr("after")})
		require.NoError(t, err)
		assert.Equal(t, StatusPartialFailure, out.Status)
		assert.Contains(t, out.RecoveryPlan, "different identity")
	})
	t.Run("update recovery", func(t *testing.T) {
		t.Parallel()
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		fake.categories[10] = inventree.Category{PK: 10, Name: "Parts"}
		fake.updateErr = errors.New("response lost")
		fake.getAfterMutation[10] = inventree.Category{PK: 99, Name: "Parts", Description: "after"}
		_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, Description: dvgoutils.Ptr("after")})
		require.NoError(t, err)
		assert.False(t, out.Recovered)
		assert.Nil(t, out.Record)
		assert.Contains(t, out.RecoveryPlan, "category_id 10")
	})
}

func TestCategoryReferenceReadsRequireExactIdentity(t *testing.T) {
	t.Parallel()
	t.Run("create parent", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		fake.categories[20] = inventree.Category{PK: 21, Name: "wrong"}
		_, _, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "Child", ParentID: dvgoutils.Ptr(20)})
		require.ErrorContains(t, err, "parent category identity verification failed")
	})
	t.Run("default location", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		fake.stockLocationResult = inventree.StockLocation{PK: 41, Name: "wrong"}
		_, _, err := createPartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "Root", DefaultLocationID: dvgoutils.Ptr(40)})
		require.ErrorContains(t, err, "default stock-location identity verification failed")
	})
	t.Run("ancestor", func(t *testing.T) {
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := newCategoryAdminFake()
		fake.categories[10] = inventree.Category{PK: 10, Name: "Moving"}
		fake.categories[20] = inventree.Category{PK: 20, Name: "Parent", Parent: dvgoutils.Ptr(30)}
		fake.categories[30] = inventree.Category{PK: 31, Name: "wrong"}
		_, _, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, ParentID: dvgoutils.Ptr(20), Confirm: true})
		require.ErrorContains(t, err, "parent category validation failed")
	})
}

func TestCategoryClearConflictReportsEveryMutuallyExclusivePair(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input UpdatePartCategoryInput
		want  string
	}{
		{name: "parent", input: UpdatePartCategoryInput{ParentID: dvgoutils.Ptr(1), ClearParent: true}, want: "parent_id"},
		{name: "default location", input: UpdatePartCategoryInput{DefaultLocationID: dvgoutils.Ptr(1), ClearDefaultLocation: true}, want: "default_location_id"},
		{name: "default keywords", input: UpdatePartCategoryInput{DefaultKeywords: dvgoutils.Ptr("x"), ClearDefaultKeywords: true}, want: "default_keywords"},
		{name: "icon", input: UpdatePartCategoryInput{Icon: dvgoutils.Ptr("x"), ClearIcon: true}, want: "icon"},
		{name: "none", input: UpdatePartCategoryInput{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, categoryClearConflict(tt.input), tt.want)
		})
	}
}

func TestCategoryMatchesCreateComparesAllSuppliedMetadata(t *testing.T) {
	t.Parallel()
	input := CreatePartCategoryInput{
		Name: "Category", Description: dvgoutils.Ptr("description"), ParentID: dvgoutils.Ptr(1),
		DefaultLocationID: dvgoutils.Ptr(2), DefaultKeywords: dvgoutils.Ptr("keywords"),
		Structural: dvgoutils.Ptr(true), Icon: dvgoutils.Ptr("icon"),
	}
	category := inventree.Category{
		PK: 10, Name: "Category", Description: "description", Parent: dvgoutils.Ptr(1),
		DefaultLocation: dvgoutils.Ptr(2), DefaultKeywords: dvgoutils.Ptr("keywords"),
		Structural: true, Icon: dvgoutils.Ptr("icon"),
	}
	assert.True(t, categoryMatchesCreate(category, input, "Category"))
	category.Icon = dvgoutils.Ptr("different")
	assert.False(t, categoryMatchesCreate(category, input, "Category"))
}

func TestUpdatePartCategoryAmbiguousRecoveryComparesExactReferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		result    inventree.Category
		input     UpdatePartCategoryInput
		recovered bool
	}{
		{name: "integer prefix mismatch", result: inventree.Category{PK: 10, Name: "Parts", Parent: dvgoutils.Ptr(1)}, input: UpdatePartCategoryInput{ID: 10, ParentID: dvgoutils.Ptr(12), Confirm: true}, recovered: false},
		{name: "default location prefix mismatch", result: inventree.Category{PK: 10, Name: "Parts", Parent: dvgoutils.Ptr(1), DefaultLocation: dvgoutils.Ptr(1)}, input: UpdatePartCategoryInput{ID: 10, DefaultLocationID: dvgoutils.Ptr(12)}, recovered: false},
		{name: "exact integer match", result: inventree.Category{PK: 10, Name: "Parts", Parent: dvgoutils.Ptr(12)}, input: UpdatePartCategoryInput{ID: 10, ParentID: dvgoutils.Ptr(12), Confirm: true}, recovered: true},
		{name: "explicit null match", result: inventree.Category{PK: 10, Name: "Parts"}, input: UpdatePartCategoryInput{ID: 10, ClearParent: true, Confirm: true}, recovered: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			a := assert.New(t)
			fake := newCategoryAdminFake()
			fake.categories[10] = inventree.Category{PK: 10, Name: "Parts", Parent: dvgoutils.Ptr(1)}
			fake.categories[1] = inventree.Category{PK: 1, Name: "Current parent"}
			fake.categories[12] = inventree.Category{PK: 12, Name: "Target"}
			fake.updateResult = tt.result
			fake.updateErr = errors.New("PATCH response lost after persist")

			_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, tt.input)
			r.NoError(err)
			a.Equal(StatusPartialFailure, out.Status)
			a.Equal(tt.recovered, out.Recovered)
		})
	}
}

func TestUpdatePartCategoryNormalizesStoredNameDuringReparentDuplicateCheck(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	fake := newCategoryAdminFake()
	fake.categories[10] = inventree.Category{PK: 10, Name: " Passives ", Parent: dvgoutils.Ptr(1)}
	fake.categories[20] = inventree.Category{PK: 20, Name: "Target"}
	fake.pages[0] = inventree.CategoryPage{Count: 1, Results: []inventree.Category{{PK: 30, Name: "Passives", Parent: dvgoutils.Ptr(20)}}}

	_, out, err := updatePartCategory(categoryAdminDeps(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: 10, ParentID: dvgoutils.Ptr(20), Confirm: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Equal("category_id", out.Clarification.Retry)
	a.Zero(fake.updateCalls)
}

type categoryAdminFake struct {
	categories          map[int]inventree.Category
	pages               map[int]inventree.CategoryPage
	pageOffsets         []int
	partCount           int
	createCalls         int
	updateCalls         int
	lastCreate          inventree.CategoryCreate
	lastPatch           inventree.PatchFields
	createResult        inventree.Category
	updateResult        inventree.Category
	createErr           error
	updateErr           error
	recoveryCategory    inventree.Category
	getAfterMutation    map[int]inventree.Category
	stockLocationResult inventree.StockLocation
}

func newCategoryAdminFake() *categoryAdminFake {
	return &categoryAdminFake{categories: map[int]inventree.Category{}, pages: map[int]inventree.CategoryPage{}, getAfterMutation: map[int]inventree.Category{}}
}

func categoryAdminDeps(fake *categoryAdminFake) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func (f *categoryAdminFake) GetPartCategory(_ context.Context, id int) (inventree.Category, error) {
	if (f.createCalls > 0 || f.updateCalls > 0) && f.getAfterMutation[id].PK != 0 {
		return f.getAfterMutation[id], nil
	}
	if f.recoveryCategory.PK == id {
		return f.recoveryCategory, nil
	}
	if category, ok := f.categories[id]; ok {
		return category, nil
	}
	return inventree.Category{}, errors.New("category not found")
}

func (f *categoryAdminFake) SearchPartCategoriesPage(_ context.Context, query inventree.CategoryQuery) (inventree.CategoryPage, error) {
	f.pageOffsets = append(f.pageOffsets, query.Offset)
	if f.createCalls > 0 && f.recoveryCategory.PK != 0 {
		return inventree.CategoryPage{Count: 1, Results: []inventree.Category{f.recoveryCategory}}, nil
	}
	if page, ok := f.pages[query.Offset]; ok {
		return page, nil
	}
	return inventree.CategoryPage{}, nil
}

func (f *categoryAdminFake) GetStockLocation(_ context.Context, id int) (inventree.StockLocation, error) {
	if f.stockLocationResult.PK != 0 {
		return f.stockLocationResult, nil
	}
	return inventree.StockLocation{PK: id, Name: "location"}, nil
}

func (f *categoryAdminFake) SearchPartsPage(_ context.Context, _ inventree.PartQuery) (inventree.PartPage, error) {
	return inventree.PartPage{Count: f.partCount}, nil
}

func (f *categoryAdminFake) CreatePartCategory(_ context.Context, input inventree.CategoryCreate) (inventree.Category, error) {
	f.createCalls++
	f.lastCreate = input
	if f.createErr != nil {
		return inventree.Category{}, f.createErr
	}
	f.categories[f.createResult.PK] = f.createResult
	return f.createResult, nil
}

func (f *categoryAdminFake) UpdatePartCategory(_ context.Context, id int, fields inventree.PatchFields) (inventree.Category, error) {
	f.updateCalls++
	f.lastPatch = fields
	f.categories[id] = f.updateResult
	return f.updateResult, f.updateErr
}

type categoryRoundTripFunc func(*http.Request) (*http.Response, error)

func (f categoryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func categoryJSONResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func newCategoryTransportClient(t *testing.T, transport http.RoundTripper) *inventree.Client {
	t.Helper()
	client, err := inventree.NewClient(inventree.Config{
		BaseURL:    "https://inventree.example",
		Credential: inventree.Credential{Scheme: inventree.AuthSchemeToken, Token: "test-token"},
		HTTPClient: &http.Client{Transport: transport},
	})
	require.NoError(t, err)
	return client
}

func categoryClientDeps(client *inventree.Client) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }}
}
