//go:build !no_integration_tests

package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMilestoneHappyPathToolsAgainstInvenTree(t *testing.T) {
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	if testenv.SkipDocker(os.Getenv) || testing.Short() {
		t.Skipf("Docker-backed InvenTree integration test excluded by %s or -short", testenv.EnvSkipDocker)
	}
	t.Parallel()

	opts := testenv.DefaultTestOptions(t)
	t.Logf("starting milestone happy-path integration stack with image %s, expected version %s, expected API %s", opts.Image, opts.ExpectedVersion, opts.ExpectedAPIVersion)
	shared, err := testenv.StartSharedInvenTree(ctx, opts)
	r.NoError(err)
	r.NotNil(shared)
	t.Cleanup(testenv.CleanupForTest(t, func() error {
		return shared.Close(context.WithoutCancel(ctx))
	}))

	t.Run("catalog_stock_supplier_and_purchase_preview_happy_path", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		location := fixture.ensure(t, testenv.FixtureLocation)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)

		_, stock, err := initialStockWorkflow(fixture.deps())(ctx, &mcp.CallToolRequest{}, InitialStockWorkflowInput{
			PartID:     part.ID,
			LocationID: location.ID,
			Quantity:   11,
			Batch:      dvgoutils.Ptr("M1H"),
			Notes:      dvgoutils.Ptr("milestone happy path"),
		})
		r.NoError(err)
		a.Equal(StatusOK, stock.Status)
		r.NotNil(stock.StockItem)
		a.Equal(part.ID, stock.StockItem.Part)
		r.NotNil(stock.StockItem.Location)
		a.Equal(location.ID, *stock.StockItem.Location)
		a.Equal(float64(11), stock.StockItem.Quantity)

		price := 1.25
		_, preview, err := previewPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, PurchasePreviewInput{
			SupplierID: supplier.ID,
			Lines: []PurchasePreviewLineInput{{
				SupplierPartID: supplierPart.ID,
				Quantity:       4,
				UnitPrice:      &price,
				Currency:       "AUD",
				Notes:          "preview only",
			}},
		})
		r.NoError(err)
		a.Equal(StatusOK, preview.Status)
		a.Equal(supplier.ID, preview.SupplierID)
		r.Len(preview.Lines, 1)
		a.Equal(part.ID, preview.Lines[0].PartID)
		a.Equal(supplierPart.ID, preview.Lines[0].SupplierPartID)
		a.Equal(5.0, *preview.Lines[0].LineTotal)

		orders, err := fixture.client.SearchPurchaseOrders(ctx, inventree.PurchaseOrderQuery{Supplier: supplier.ID})
		r.NoError(err)
		a.Empty(orders, "purchase preview must not create purchase orders")
	})

	t.Run("part_exact_detail_and_scalar_maintenance", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		name, err := fixture.run.Name("part-exact-detail")
		r.NoError(err)
		link := "https://example.test/parts/integration?source=mcp#detail"

		_, created, err := createPart(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartInput{
			Name: name, CategoryID: category.ID, Consumable: dvgoutils.Ptr(true), DefaultExpiry: dvgoutils.Ptr(30),
			IsTemplate: dvgoutils.Ptr(false), Keywords: dvgoutils.Ptr("integration keywords"), Link: &link,
			Locked: dvgoutils.Ptr(false), MinimumStock: dvgoutils.Ptr(2.5), MaximumStock: dvgoutils.Ptr(5.5),
			Revision: dvgoutils.Ptr("A"), Salable: dvgoutils.Ptr(true), Testable: dvgoutils.Ptr(true), Notes: dvgoutils.Ptr("integration markdown"),
		})
		r.NoError(err)
		a.Equal(StatusOK, created.Status)
		a.Equal(link, created.Record.Link)
		a.Equal("integration markdown", *created.Record.Notes)
		r.NotNil(created.Record.CreationUser)

		_, exact, err := getPart(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: created.Record.PK})
		r.NoError(err)
		a.Equal(StatusOK, exact.Status)
		a.Equal(link, exact.Record.Link)
		a.Equal(2.5, exact.Record.MinimumStock)
		a.Equal(5.5, exact.Record.MaximumStock)
		a.True(exact.Record.Consumable)

		_, updated, err := updatePart(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartInput{
			ID: created.Record.PK, ClearKeywords: true, ClearLink: true, ClearRevision: true, ClearNotes: true,
			DefaultExpiry: dvgoutils.Ptr(0), MinimumStock: dvgoutils.Ptr(3.0), MaximumStock: dvgoutils.Ptr(0.0),
			Consumable: dvgoutils.Ptr(false), Salable: dvgoutils.Ptr(false), Testable: dvgoutils.Ptr(false),
		})
		r.NoError(err)
		a.Equal(StatusOK, updated.Status)
		a.Nil(updated.Record.Keywords)
		a.Empty(updated.Record.Link)
		a.Nil(updated.Record.Revision)
		a.Nil(updated.Record.Notes)
		a.Zero(updated.Record.DefaultExpiry)
		a.Equal(3.0, updated.Record.MinimumStock)
		a.Zero(updated.Record.MaximumStock)
		a.False(updated.Record.Consumable)
		a.False(updated.Record.Salable)
		a.False(updated.Record.Testable)
	})

	t.Run("part_family_relationships", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		newPart := func(suffix string, template bool) inventree.Part {
			name, err := fixture.run.Name(suffix)
			r.NoError(err)
			part, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: name, Category: &category.ID, IsTemplate: dvgoutils.Ptr(template)})
			r.NoError(err)
			r.NotZero(part.PK)
			return part
		}
		template := newPart("part-family-template", true)
		root := newPart("part-family-root", false)
		middle := newPart("part-family-middle", false)
		leaf := newPart("part-family-leaf", false)
		nonTemplateTarget := newPart("part-family-non-template-target", false)
		otherTemplate := newPart("part-family-other-template", true)
		assertRejected := func(fields inventree.PatchFields) {
			_, updateErr := fixture.client.UpdatePart(ctx, leaf.PK, fields)
			r.Error(updateErr)
			var apiErr *inventree.APIError
			r.ErrorAs(updateErr, &apiErr)
			a.Equal(http.StatusBadRequest, apiErr.StatusCode)
		}

		assertRejected(inventree.PatchFields{"revision_of": inventree.Set(nonTemplateTarget.PK)})
		_, err := fixture.client.UpdatePart(ctx, leaf.PK, inventree.PatchFields{"revision": inventree.Set("C")})
		r.NoError(err)
		assertRejected(inventree.PatchFields{"revision_of": inventree.Set(template.PK)})
		_, err = fixture.client.UpdatePart(ctx, leaf.PK, inventree.PatchFields{"variant_of": inventree.Set(otherTemplate.PK)})
		r.NoError(err)
		_, err = fixture.client.UpdatePart(ctx, nonTemplateTarget.PK, inventree.PatchFields{"variant_of": inventree.Set(template.PK)})
		r.NoError(err)
		assertRejected(inventree.PatchFields{"revision_of": inventree.Set(nonTemplateTarget.PK)})
		assertRejected(inventree.PatchFields{"variant_of": inventree.Set(nonTemplateTarget.PK)})
		assertRejected(inventree.PatchFields{"revision_of": inventree.Set(2147483647)})

		cycleA := newPart("part-family-cycle-a", false)
		cycleB := newPart("part-family-cycle-b", false)
		_, err = fixture.client.UpdatePart(ctx, cycleA.PK, inventree.PatchFields{"revision": inventree.Set("A"), "variant_of": inventree.Set(template.PK)})
		r.NoError(err)
		_, err = fixture.client.UpdatePart(ctx, cycleB.PK, inventree.PatchFields{"revision": inventree.Set("B"), "variant_of": inventree.Set(template.PK), "revision_of": inventree.Set(cycleA.PK)})
		r.NoError(err)
		_, err = fixture.client.UpdatePart(ctx, cycleA.PK, inventree.PatchFields{"revision_of": inventree.Set(cycleB.PK)})
		r.NoError(err, "pinned InvenTree accepts revision cycles, so the MCP must enforce this guard")
		_, existingCycle, err := updatePartFamilyRelationships(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartFamilyRelationshipsInput{ID: cycleA.PK, ClearVariantOf: true, DryRun: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, existingCycle.Status)
		r.NotNil(existingCycle.Clarification)
		a.Equal("revision_of_id", existingCycle.Clarification.Field)
		a.Contains(existingCycle.Clarification.Reason, "cycle")

		_, err = fixture.client.UpdatePart(ctx, root.PK, inventree.PatchFields{"variant_of": inventree.Set(template.PK)})
		r.NoError(err)
		_, err = fixture.client.UpdatePart(ctx, middle.PK, inventree.PatchFields{"revision": inventree.Set("B"), "revision_of": inventree.Set(root.PK), "variant_of": inventree.Set(template.PK)})
		r.NoError(err)
		_, err = fixture.client.UpdatePart(ctx, leaf.PK, inventree.PatchFields{"revision": inventree.Set("C")})
		r.NoError(err)

		input := UpdatePartFamilyRelationshipsInput{ID: leaf.PK, RevisionOfID: &middle.PK, VariantOfID: &template.PK, DryRun: true}
		_, preview, err := updatePartFamilyRelationships(fixture.deps())(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusOK, preview.Status)
		r.NotNil(preview.Plan)
		a.Equal(&middle.PK, preview.Plan.After.RevisionOf)
		a.Equal(&template.PK, preview.Plan.After.VariantOf)
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = preview.PlanHash
		_, updated, err := updatePartFamilyRelationships(fixture.deps())(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusOK, updated.Status)
		r.NotNil(updated.Record)
		a.Equal(&middle.PK, updated.Record.RevisionOf)
		a.Equal(&template.PK, updated.Record.VariantOf)

		revisions, err := fixture.client.SearchPartsByQuery(ctx, inventree.PartQuery{RevisionOf: middle.PK})
		r.NoError(err)
		a.Contains(partIDs(revisions), leaf.PK)
		variants, err := fixture.client.SearchPartsByQuery(ctx, inventree.PartQuery{VariantOf: template.PK})
		r.NoError(err)
		a.Contains(partIDs(variants), leaf.PK)
		middleDetail, err := fixture.client.GetPartDetail(ctx, middle.PK)
		r.NoError(err)
		r.NotNil(middleDetail.RevisionCount)
		a.GreaterOrEqual(*middleDetail.RevisionCount, 1)

		_, cycle, err := updatePartFamilyRelationships(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartFamilyRelationshipsInput{ID: root.PK, RevisionOfID: &leaf.PK, DryRun: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, cycle.Status)
		a.Contains(cycle.Clarification.Reason, "cycle")

		clear := UpdatePartFamilyRelationshipsInput{ID: leaf.PK, ClearRevisionOf: true, ClearVariantOf: true, DryRun: true}
		_, clearPreview, err := updatePartFamilyRelationships(fixture.deps())(ctx, &mcp.CallToolRequest{}, clear)
		r.NoError(err)
		clear.DryRun = false
		clear.Confirm = true
		clear.PlanHash = clearPreview.PlanHash
		_, cleared, err := updatePartFamilyRelationships(fixture.deps())(ctx, &mcp.CallToolRequest{}, clear)
		r.NoError(err)
		a.Equal(StatusOK, cleared.Status)
		a.Nil(cleared.Record.RevisionOf)
		a.Nil(cleared.Record.VariantOf)
	})

	t.Run("part_relation_administration", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		newPart := func(suffix string) inventree.Part {
			name, err := fixture.run.Name(suffix)
			r.NoError(err)
			part, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: name, Category: &category.ID, Active: dvgoutils.Ptr(true)})
			r.NoError(err)
			return part
		}
		part1, part2 := newPart("relation-tool-one"), newPart("relation-tool-two")
		_, created, err := createPartRelation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: part1.PK, Part2ID: part2.PK, Note: "tool-created"})
		r.NoError(err)
		a.Equal(StatusOK, created.Status)
		r.NotNil(created.Record)
		a.True(created.Verified)

		_, duplicate, err := createPartRelation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartRelationInput{Part1ID: part2.PK, Part2ID: part1.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, duplicate.Status)
		r.Len(duplicate.Candidates, 1)
		a.Equal(created.Record.PK, duplicate.Candidates[0].PK)

		_, listed, err := listPartRelations(fixture.deps())(ctx, &mcp.CallToolRequest{}, ListPartRelationsInput{PartID: part2.PK})
		r.NoError(err)
		a.Equal(StatusOK, listed.Status)
		r.Len(listed.Results, 1)
		a.Equal(created.Record.PK, listed.Results[0].PK)
		_, exact, err := getPartRelation(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: created.Record.PK})
		r.NoError(err)
		a.Equal(StatusOK, exact.Status)
		a.Equal("tool-created", exact.Record.Note)

		note := "updated note"
		_, updatePlan, err := updatePartRelation(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: created.Record.PK, Note: &note})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, updatePlan.Status)
		_, updated, err := updatePartRelation(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartRelationInput{ID: created.Record.PK, Note: &note, Confirm: true, PlanHash: updatePlan.PlanHash})
		r.NoError(err)
		a.Equal(StatusOK, updated.Status)
		a.Equal(note, updated.Record.Note)

		_, deletePlan, err := deletePartRelation(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: created.Record.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, deletePlan.Status)
		_, deleted, err := deletePartRelation(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePartRelationInput{ID: created.Record.PK, Confirm: true, PlanHash: deletePlan.PlanHash})
		r.NoError(err)
		a.Equal(StatusOK, deleted.Status)
		a.True(deleted.Verified)

		part1, err = fixture.client.UpdatePart(ctx, part1.PK, inventree.PatchFields{"active": inventree.Set(false)})
		r.NoError(err)
		a.False(part1.Active)
		_, deletePartPreview, err := deletePart(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: part1.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, deletePartPreview.Status)
		a.Empty(deletePartPreview.RelatedParts, "verified relation deletion must clear the MCP dependency preflight")
		a.Nil(deletePartPreview.Blocking)
		_, deletedPart, err := deletePart(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePartInput{ID: part1.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusOK, deletedPart.Status)
		a.True(deletedPart.Verified)
	})

	t.Run("part_category_administration", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		location := fixture.ensure(t, testenv.FixtureLocation)

		rootName, err := fixture.run.Name("category-admin-root")
		r.NoError(err)
		_, rootOut, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: rootName, DefaultLocationID: &location.ID, DefaultKeywords: dvgoutils.Ptr("root"), Structural: dvgoutils.Ptr(false)})
		r.NoError(err)
		a.Equal(StatusOK, rootOut.Status)
		r.NotNil(rootOut.Record)
		root := *rootOut.Record
		a.Nil(root.Parent)
		r.NotNil(root.DefaultLocation)
		a.Equal(location.ID, *root.DefaultLocation)
		r.NotNil(root.DefaultKeywords)
		a.Equal("root", *root.DefaultKeywords)

		_, rootDuplicate, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: "  " + strings.ToUpper(rootName) + "  "})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, rootDuplicate.Status)

		otherParentName, err := fixture.run.Name("category-admin-other-parent")
		r.NoError(err)
		_, otherParentOut, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: otherParentName})
		r.NoError(err)
		r.NotNil(otherParentOut.Record)
		otherParent := *otherParentOut.Record
		_, sameNameOtherParent, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: rootName, ParentID: &otherParent.PK})
		r.NoError(err)
		a.Equal(StatusOK, sameNameOtherParent.Status)

		childName, err := fixture.run.Name("category-admin-child")
		r.NoError(err)
		_, childOut, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: childName, ParentID: &root.PK, Description: dvgoutils.Ptr("child")})
		r.NoError(err)
		r.NotNil(childOut.Record)
		child := *childOut.Record
		a.Equal(root.PK, *child.Parent)

		_, exact, err := getPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: child.PK})
		r.NoError(err)
		a.Equal(StatusOK, exact.Status)
		a.Equal(child.PK, exact.Record.PK)
		r.NotNil(exact.Record.Parent)
		a.Equal(root.PK, *exact.Record.Parent)
		a.Equal("child", exact.Record.Description)
		a.NotEmpty(exact.Record.PathString)
		a.NotEmpty(exact.Record.Path)
		r.NotNil(exact.Record.ParentDefaultLocation)
		a.Equal(location.ID, *exact.Record.ParentDefaultLocation)
		r.NotNil(exact.Record.PartCount)
		r.NotNil(exact.Record.Subcategories)

		_, partial, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: child.PK, Description: dvgoutils.Ptr("updated child"), DefaultKeywords: dvgoutils.Ptr("")})
		r.NoError(err)
		a.Equal(StatusOK, partial.Status)
		a.Equal("updated child", partial.Record.Description)
		r.NotNil(partial.Record.DefaultKeywords)
		a.Equal("", *partial.Record.DefaultKeywords)

		descendantName, err := fixture.run.Name("category-admin-descendant")
		r.NoError(err)
		_, descendantOut, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: descendantName, ParentID: &child.PK})
		r.NoError(err)
		r.NotNil(descendantOut.Record)
		descendant := *descendantOut.Record
		partName, err := fixture.run.Name("category-admin-part")
		r.NoError(err)
		_, err = fixture.client.CreatePart(ctx, inventree.PartCreate{Name: partName, Category: &child.PK})
		r.NoError(err)

		_, needsConfirm, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: child.PK, ParentID: &otherParent.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, needsConfirm.Status)
		r.NotNil(needsConfirm.Hierarchy)
		a.GreaterOrEqual(needsConfirm.Hierarchy.DirectPartCount, 1)
		a.GreaterOrEqual(needsConfirm.Hierarchy.DirectChildCount, 1)
		_, reparented, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: child.PK, ParentID: &otherParent.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusOK, reparented.Status)
		a.Equal(otherParent.PK, *reparented.Record.Parent)

		collisionName, err := fixture.run.Name("category-admin-collision")
		r.NoError(err)
		_, collisionOut, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: collisionName, ParentID: &otherParent.PK})
		r.NoError(err)
		r.NotNil(collisionOut.Record)
		_, renameCollision, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: child.PK, Name: &collisionName})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, renameCollision.Status)

		moveName, err := fixture.run.Name("category-admin-move-collision")
		r.NoError(err)
		_, movingOut, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: moveName, ParentID: &root.PK})
		r.NoError(err)
		r.NotNil(movingOut.Record)
		_, destinationCollision, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: moveName, ParentID: &otherParent.PK})
		r.NoError(err)
		r.NotNil(destinationCollision.Record)
		_, reparentCollision, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: movingOut.Record.PK, ParentID: &otherParent.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, reparentCollision.Status)

		_, cycle, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: child.PK, ParentID: &descendant.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, cycle.Status)
		a.Contains(cycle.Clarification.Reason, "descendant")
		_, selfParent, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: child.PK, ParentID: &child.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, selfParent.Status)
		a.Contains(selfParent.Clarification.Reason, "own parent")

		_, structural, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: child.PK, Structural: dvgoutils.Ptr(true), Confirm: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, structural.Status)
		a.Contains(structural.Clarification.Reason, "directly assigned parts")

		_, cleared, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: root.PK, ClearDefaultLocation: true, ClearDefaultKeywords: true})
		r.NoError(err)
		a.Equal(StatusOK, cleared.Status)
		a.Nil(cleared.Record.DefaultLocation)
		a.Nil(cleared.Record.DefaultKeywords)

		_, invalidLocation, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: rootName, DefaultLocationID: dvgoutils.Ptr(99999999)})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, invalidLocation.Status)
		_, invalidParent, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: rootName + "-invalid-parent", ParentID: dvgoutils.Ptr(99999999)})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, invalidParent.Status)
		_, invalidUpdateLocation, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: root.PK, DefaultLocationID: dvgoutils.Ptr(99999999)})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, invalidUpdateLocation.Status)
		_, invalidUpdateParent, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: root.PK, ParentID: dvgoutils.Ptr(99999999), Confirm: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, invalidUpdateParent.Status)

		emptyName, err := fixture.run.Name("category-admin-empty-structural")
		r.NoError(err)
		_, emptyOut, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: emptyName, Structural: dvgoutils.Ptr(false)})
		r.NoError(err)
		r.NotNil(emptyOut.Record)
		_, allowedStructural, err := updatePartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: emptyOut.Record.PK, Structural: dvgoutils.Ptr(true), Confirm: true})
		r.NoError(err)
		a.Equal(StatusOK, allowedStructural.Status)
		a.True(allowedStructural.Record.Structural)

		lostCreateName, err := fixture.run.Name("category-admin-lost-create")
		r.NoError(err)
		lostCreateClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPost, path: "/api/part/category/"}})
		r.NoError(err)
		_, lostCreate, err := createPartCategory(categoryClientDeps(lostCreateClient))(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: lostCreateName, Description: dvgoutils.Ptr("persisted despite lost response")})
		r.NoError(err)
		a.Equal(StatusPartialFailure, lostCreate.Status)
		a.True(lostCreate.Recovered)
		r.NotNil(lostCreate.Record)

		lostPatchClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPatch, path: fmt.Sprintf("/api/part/category/%d/", lostCreate.Record.PK)}})
		r.NoError(err)
		_, lostPatch, err := updatePartCategory(categoryClientDeps(lostPatchClient))(ctx, &mcp.CallToolRequest{}, UpdatePartCategoryInput{ID: lostCreate.Record.PK, Description: dvgoutils.Ptr("patch persisted despite lost response")})
		r.NoError(err)
		a.Equal(StatusPartialFailure, lostPatch.Status)
		a.True(lostPatch.Recovered)
		a.Equal("patch persisted despite lost response", lostPatch.Record.Description)

		pagedParentName, err := fixture.run.Name("category-admin-paged-parent")
		r.NoError(err)
		pagedParent, err := fixture.client.CreatePartCategory(ctx, inventree.CategoryCreate{Name: pagedParentName})
		r.NoError(err)
		var lastName string
		for i := 0; i < MaxLookupLimit+1; i++ {
			lastName, err = fixture.run.Name(fmt.Sprintf("category-admin-page-%03d", i))
			r.NoError(err)
			_, err = fixture.client.CreatePartCategory(ctx, inventree.CategoryCreate{Name: lastName, Parent: &pagedParent.PK})
			r.NoError(err)
		}
		_, laterPageDuplicate, err := createPartCategory(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePartCategoryInput{Name: strings.ToUpper(lastName), ParentID: &pagedParent.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, laterPageDuplicate.Status)
		a.Equal("category_id", laterPageDuplicate.Clarification.Retry)
	})

	t.Run("stock_location_and_metadata_administration", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		ownerPK := currentUserOwnerPK(t, ctx, fixture.client)
		part := fixture.ensure(t, testenv.FixturePart)

		typeName, err := fixture.run.Name("stock-admin-type")
		r.NoError(err)
		var locationType inventree.StockLocationType
		r.NoError(fixture.client.Post(ctx, "/api/stock/location-type/", map[string]any{"name": typeName, "description": "F-S21 tool integration type", "icon": ""}, &locationType))
		r.NotZero(locationType.PK)

		rootName, err := fixture.run.Name("stock-admin-root")
		r.NoError(err)
		_, rootOut, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: rootName, Description: dvgoutils.Ptr("root"), Structural: dvgoutils.Ptr(false), External: dvgoutils.Ptr(false)})
		r.NoError(err)
		r.NotNil(rootOut.Record)
		root := *rootOut.Record

		childName, err := fixture.run.Name("stock-admin-child")
		r.NoError(err)
		_, childOut, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: childName, Description: dvgoutils.Ptr("child"), ParentID: &root.PK, OwnerID: &ownerPK, LocationTypeID: &locationType.PK, Structural: dvgoutils.Ptr(false), External: dvgoutils.Ptr(false)})
		r.NoError(err)
		r.NotNil(childOut.Record)
		child := *childOut.Record
		r.Equal(root.PK, *child.Parent)
		r.Equal(ownerPK, *child.Owner)
		r.Equal(locationType.PK, *child.LocationType)

		matrixName, err := fixture.run.Name("stock-admin-namespace")
		r.NoError(err)
		_, matrixRoot, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: matrixName})
		r.NoError(err)
		a.Equal(StatusOK, matrixRoot.Status)
		_, matrixUnderRoot, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: matrixName, ParentID: &root.PK})
		r.NoError(err)
		a.Equal(StatusOK, matrixUnderRoot.Status)
		_, matrixUnderChild, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: matrixName, ParentID: &child.PK})
		r.NoError(err)
		a.Equal(StatusOK, matrixUnderChild.Status)
		_, duplicateRoot, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: "  " + strings.ToUpper(matrixName) + "  "})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, duplicateRoot.Status)
		_, duplicateChild, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: strings.ToUpper(matrixName), ParentID: &root.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, duplicateChild.Status)

		_, exactLocation, err := getStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: child.PK})
		r.NoError(err)
		a.Equal(StatusOK, exactLocation.Status)
		a.Equal(child.PK, exactLocation.Record.PK)
		_, exactType, err := getStockLocationType(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: locationType.PK})
		r.NoError(err)
		a.Equal(typeName, exactType.Record.Name)

		_, invalidReference, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: rootName + "-invalid", OwnerID: dvgoutils.Ptr(99999999), LocationTypeID: dvgoutils.Ptr(99999998)})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, invalidReference.Status)

		updatedName := childName + "-updated"
		_, ordinaryUpdate, err := updateStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateStockLocationInput{ID: child.PK, Name: &updatedName, Description: dvgoutils.Ptr("ordinary update"), ClearLocationType: true})
		r.NoError(err)
		r.NotNil(ordinaryUpdate.Record)
		a.Equal(updatedName, ordinaryUpdate.Record.Name)
		a.Equal(ownerPK, *ordinaryUpdate.Record.Owner)
		a.Nil(ordinaryUpdate.Record.LocationType)
		child = *ordinaryUpdate.Record

		_, clearedOwner, err := assignOwner(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: child.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, clearedOwner.Status)
		r.NotNil(clearedOwner.Plan)
		_, clearedOwner, err = assignOwner(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: OwnerObjectStockLocation, ObjectID: child.PK, Confirm: true, PlanHash: clearedOwner.PlanHash})
		r.NoError(err)
		a.Equal(StatusOK, clearedOwner.Status)
		a.True(clearedOwner.Verified)
		a.Nil(clearedOwner.OwnerID)
		refreshedChild, err := fixture.client.GetStockLocation(ctx, child.PK)
		r.NoError(err)
		a.Nil(refreshedChild.Owner)
		child = refreshedChild

		targetName, err := fixture.run.Name("stock-admin-reparent-target")
		r.NoError(err)
		_, targetOut, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: targetName})
		r.NoError(err)
		r.NotNil(targetOut.Record)
		target := *targetOut.Record
		_, restructurePlan, err := restructureStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, RestructureStockLocationInput{ID: child.PK, ParentID: &target.PK, Structural: dvgoutils.Ptr(true), External: dvgoutils.Ptr(true), DryRun: true})
		r.NoError(err)
		r.NotEmpty(restructurePlan.PlanHash)
		r.NotNil(restructurePlan.Plan.TargetParent)
		a.Equal(target.PK, restructurePlan.Plan.TargetParent.ID)
		a.Equal(target.PK, *restructurePlan.Plan.After.ParentID)
		_, restructured, err := restructureStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, RestructureStockLocationInput{ID: child.PK, ParentID: &target.PK, Structural: dvgoutils.Ptr(true), External: dvgoutils.Ptr(true), Confirm: true, PlanHash: restructurePlan.PlanHash})
		r.NoError(err)
		a.Equal(StatusOK, restructured.Status)
		a.Equal(target.PK, *restructured.Record.Parent)
		a.True(restructured.Record.Structural)
		a.True(restructured.Record.External)

		_, cycle, err := restructureStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, RestructureStockLocationInput{ID: target.PK, ParentID: &child.PK, DryRun: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, cycle.Status)

		_, nonStructuralPlan, err := restructureStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, RestructureStockLocationInput{ID: child.PK, Structural: dvgoutils.Ptr(false), DryRun: true})
		r.NoError(err)
		_, childReady, err := restructureStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, RestructureStockLocationInput{ID: child.PK, Structural: dvgoutils.Ptr(false), Confirm: true, PlanHash: nonStructuralPlan.PlanHash})
		r.NoError(err)
		a.False(childReady.Record.Structural)

		stock, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: child.PK, Quantity: 3})
		r.NoError(err)
		metadataInput := UpdateStockItemMetadataInput{ID: stock.PK, Batch: dvgoutils.Ptr("F-S21"), ExpiryDate: dvgoutils.Ptr("2027-01-02"), Packaging: dvgoutils.Ptr("tray"), Notes: dvgoutils.Ptr("reviewed metadata"), Link: dvgoutils.Ptr("https://example.test/stock"), DryRun: true}
		_, metadataPlan, err := updateStockItemMetadata(fixture.deps())(ctx, &mcp.CallToolRequest{}, metadataInput)
		r.NoError(err)
		r.NotEmpty(metadataPlan.PlanHash)
		_, err = fixture.client.UpdateStockItem(ctx, stock.PK, inventree.PatchFields{"quantity": inventree.Set(4.0)})
		r.NoError(err)
		metadataInput.DryRun = false
		metadataInput.Confirm = true
		metadataInput.PlanHash = metadataPlan.PlanHash
		_, staleMetadata, err := updateStockItemMetadata(fixture.deps())(ctx, &mcp.CallToolRequest{}, metadataInput)
		r.NoError(err)
		a.Equal(StatusClarificationRequired, staleMetadata.Status)
		currentStock, err := fixture.client.GetStockItem(ctx, stock.PK)
		r.NoError(err)
		if currentStock.Batch != nil {
			a.Empty(*currentStock.Batch)
		}

		metadataInput.DryRun = true
		metadataInput.Confirm = false
		metadataInput.PlanHash = ""
		_, metadataPlan, err = updateStockItemMetadata(fixture.deps())(ctx, &mcp.CallToolRequest{}, metadataInput)
		r.NoError(err)
		metadataInput.DryRun = false
		metadataInput.Confirm = true
		metadataInput.PlanHash = metadataPlan.PlanHash
		_, metadataOut, err := updateStockItemMetadata(fixture.deps())(ctx, &mcp.CallToolRequest{}, metadataInput)
		r.NoError(err)
		a.Equal(StatusOK, metadataOut.Status)
		r.NotNil(metadataOut.Record)
		a.Equal("F-S21", *metadataOut.Record.Batch)
		a.Equal("2027-01-02", *metadataOut.Record.ExpiryDate)
		a.Equal("tray", *metadataOut.Record.Packaging)

		lostCreateName, err := fixture.run.Name("stock-admin-lost-create")
		r.NoError(err)
		lostCreateClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPost, path: "/api/stock/location/"}})
		r.NoError(err)
		lostFixture := fixture
		lostFixture.client = lostCreateClient
		_, lostCreate, err := createStockLocation(lostFixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: lostCreateName, Description: dvgoutils.Ptr("persisted despite lost response")})
		r.NoError(err)
		a.Equal(StatusOK, lostCreate.Status)
		a.True(lostCreate.Recovered)
		r.NotNil(lostCreate.Record)

		lostPatchClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPatch, path: fmt.Sprintf("/api/stock/location/%d/", lostCreate.Record.PK)}})
		r.NoError(err)
		lostFixture.client = lostPatchClient
		_, lostPatch, err := updateStockLocation(lostFixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateStockLocationInput{ID: lostCreate.Record.PK, Description: dvgoutils.Ptr("ordinary patch persisted")})
		r.NoError(err)
		a.Equal(StatusOK, lostPatch.Status)
		a.True(lostPatch.Recovered)
		a.Equal("ordinary patch persisted", lostPatch.Record.Description)

		_, lostRestructurePlan, err := restructureStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, RestructureStockLocationInput{ID: lostCreate.Record.PK, Structural: dvgoutils.Ptr(true), DryRun: true})
		r.NoError(err)
		lostRestructureClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPatch, path: fmt.Sprintf("/api/stock/location/%d/", lostCreate.Record.PK)}})
		r.NoError(err)
		lostFixture.client = lostRestructureClient
		_, lostRestructure, err := restructureStockLocation(lostFixture.deps())(ctx, &mcp.CallToolRequest{}, RestructureStockLocationInput{ID: lostCreate.Record.PK, Structural: dvgoutils.Ptr(true), Confirm: true, PlanHash: lostRestructurePlan.PlanHash})
		r.NoError(err)
		a.Equal(StatusOK, lostRestructure.Status)
		a.True(lostRestructure.Recovered)
		a.True(lostRestructure.Record.Structural)

		lostMetadataInput := UpdateStockItemMetadataInput{ID: stock.PK, Batch: dvgoutils.Ptr("F-S21-recovered"), DryRun: true}
		_, lostMetadataPlan, err := updateStockItemMetadata(fixture.deps())(ctx, &mcp.CallToolRequest{}, lostMetadataInput)
		r.NoError(err)
		lostMetadataClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPatch, path: fmt.Sprintf("/api/stock/%d/", stock.PK)}})
		r.NoError(err)
		lostFixture.client = lostMetadataClient
		lostMetadataInput.DryRun = false
		lostMetadataInput.Confirm = true
		lostMetadataInput.PlanHash = lostMetadataPlan.PlanHash
		_, lostMetadata, err := updateStockItemMetadata(lostFixture.deps())(ctx, &mcp.CallToolRequest{}, lostMetadataInput)
		r.NoError(err)
		a.Equal(StatusOK, lostMetadata.Status)
		a.True(lostMetadata.Recovered)
		a.Equal("F-S21-recovered", *lostMetadata.Record.Batch)

		_, exactStock, err := getStockItem(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: stock.PK})
		r.NoError(err)
		a.Equal(stock.PK, exactStock.Record.PK)
		a.Equal(float64(4), exactStock.Record.Quantity)

		duplicateName, err := fixture.run.Name("page-duplicate")
		r.NoError(err)
		for i := 0; i < stockLocationPageSize; i++ {
			var filler inventree.StockLocation
			r.NoError(fixture.client.Post(ctx, "/api/stock/location/", map[string]any{"name": fmt.Sprintf("%s-%03d", duplicateName, i), "parent": root.PK, "structural": false, "external": false}, &filler))
			r.NotZero(filler.PK)
		}
		var laterDuplicate inventree.StockLocation
		r.NoError(fixture.client.Post(ctx, "/api/stock/location/", map[string]any{"name": duplicateName, "parent": root.PK, "structural": false, "external": false}, &laterDuplicate))
		_, duplicateOut, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: "  " + strings.ToUpper(duplicateName) + "  ", ParentID: &root.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, duplicateOut.Status)
		r.NotNil(duplicateOut.Clarification)
		a.Contains(clarificationCandidateIDs(*duplicateOut.Clarification), fmt.Sprint(laterDuplicate.PK))
	})

	t.Run("stock_provenance_correction", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		location := fixture.ensure(t, testenv.FixtureLocation)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		order := fixture.ensure(t, testenv.FixturePurchaseOrder)

		stock, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 3})
		r.NoError(err)
		_, err = fixture.client.UpdateStockItem(ctx, stock.PK, inventree.PatchFields{
			"supplier_part":           inventree.Set(supplierPart.ID),
			"purchase_order":          inventree.Set(order.ID),
			"purchase_price":          inventree.Set("1.25"),
			"purchase_price_currency": inventree.Set("AUD"),
		})
		r.NoError(err)

		plannedInput := UpdateStockItemProvenanceInput{StockItemID: stock.PK, PurchasePrice: dvgoutils.Ptr("2.50"), DryRun: true}
		_, plan, err := updateStockItemProvenance(fixture.deps())(ctx, &mcp.CallToolRequest{}, plannedInput)
		r.NoError(err)
		a.Equal(StatusOK, plan.Status)
		a.Equal(part.ID, plan.Plan.Before.PartID)
		a.Equal(3.0, plan.Plan.Before.Quantity)
		a.Equal(supplierPart.ID, *plan.Plan.Before.SupplierPartID)
		a.Equal(order.ID, *plan.Plan.Before.PurchaseOrderID)
		a.Equal("AUD", plan.Plan.Before.PriceCurrency)

		_, err = fixture.client.UpdateStockItem(ctx, stock.PK, inventree.PatchFields{"quantity": inventree.Set(4.0)})
		r.NoError(err)
		plannedInput.DryRun = false
		plannedInput.Confirm = true
		plannedInput.PlanHash = plan.PlanHash
		_, stale, err := updateStockItemProvenance(fixture.deps())(ctx, &mcp.CallToolRequest{}, plannedInput)
		r.NoError(err)
		a.Equal(StatusClarificationRequired, stale.Status)

		plannedInput.DryRun = true
		plannedInput.Confirm = false
		plannedInput.PlanHash = ""
		_, plan, err = updateStockItemProvenance(fixture.deps())(ctx, &mcp.CallToolRequest{}, plannedInput)
		r.NoError(err)
		plannedInput.DryRun = false
		plannedInput.Confirm = true
		plannedInput.PlanHash = plan.PlanHash
		_, corrected, err := updateStockItemProvenance(fixture.deps())(ctx, &mcp.CallToolRequest{}, plannedInput)
		r.NoError(err)
		a.Equal(StatusOK, corrected.Status)
		a.Equal("2.5", string(*corrected.Record.PurchasePrice))

		otherSupplierName, err := fixture.run.Name("stock-provenance-other-supplier")
		r.NoError(err)
		otherSupplier, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{Name: otherSupplierName, Currency: "AUD", IsSupplier: true})
		r.NoError(err)
		otherOrder, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: otherSupplier.PK})
		r.NoError(err)
		_, mismatch, err := updateStockItemProvenance(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateStockItemProvenanceInput{StockItemID: stock.PK, PurchaseOrderID: &otherOrder.PK, DryRun: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, mismatch.Status)
		a.Contains(mismatch.Clarification.Reason, "supplier")

		trackingBefore, err := fixture.client.SearchStockTrackingPage(ctx, inventree.StockTrackingQuery{ItemID: stock.PK, Limit: 100})
		r.NoError(err)
		clearInput := UpdateStockItemProvenanceInput{StockItemID: stock.PK, ClearSupplierPart: true, ClearPurchaseOrder: true, ClearPurchasePrice: true, PurchasePriceCurrency: dvgoutils.Ptr("USD"), DryRun: true}
		_, clearPlan, err := updateStockItemProvenance(fixture.deps())(ctx, &mcp.CallToolRequest{}, clearInput)
		r.NoError(err)
		clearInput.DryRun = false
		clearInput.Confirm = true
		clearInput.PlanHash = clearPlan.PlanHash
		_, cleared, err := updateStockItemProvenance(fixture.deps())(ctx, &mcp.CallToolRequest{}, clearInput)
		r.NoError(err)
		a.Equal(StatusOK, cleared.Status)
		a.Nil(cleared.Record.SupplierPart)
		a.Nil(cleared.Record.PurchaseOrder)
		a.Nil(cleared.Record.PurchasePrice)
		a.Equal("USD", cleared.Record.PurchasePriceCurrency)
		trackingAfter, err := fixture.client.SearchStockTrackingPage(ctx, inventree.StockTrackingQuery{ItemID: stock.PK, Limit: 100})
		r.NoError(err)
		a.Len(trackingAfter.Results, len(trackingBefore.Results), "provenance PATCH must not imply a rewritten historical tracking event")

		ambiguousStock, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 1})
		r.NoError(err)
		ambiguousInput := UpdateStockItemProvenanceInput{StockItemID: ambiguousStock.PK, PurchasePrice: dvgoutils.Ptr("3.75"), DryRun: true}
		_, ambiguousPlan, err := updateStockItemProvenance(fixture.deps())(ctx, &mcp.CallToolRequest{}, ambiguousInput)
		r.NoError(err)
		lostClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPatch, path: fmt.Sprintf("/api/stock/%d/", ambiguousStock.PK)}})
		r.NoError(err)
		lostFixture := fixture
		lostFixture.client = lostClient
		ambiguousInput.DryRun = false
		ambiguousInput.Confirm = true
		ambiguousInput.PlanHash = ambiguousPlan.PlanHash
		_, recovered, err := updateStockItemProvenance(lostFixture.deps())(ctx, &mcp.CallToolRequest{}, ambiguousInput)
		r.NoError(err)
		a.Equal(StatusOK, recovered.Status)
		a.True(recovered.Recovered)
		a.Equal("3.75", string(*recovered.Record.PurchasePrice))
	})

	t.Run("stock_item_detail_completeness", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)

		rootName, err := fixture.run.Name("stock-detail-root")
		r.NoError(err)
		_, rootOut, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: rootName})
		r.NoError(err)
		r.NotNil(rootOut.Record)
		root := *rootOut.Record

		childName, err := fixture.run.Name("stock-detail-child")
		r.NoError(err)
		_, childOut, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: childName, ParentID: &root.PK})
		r.NoError(err)
		r.NotNil(childOut.Record)
		child := *childOut.Record

		_, created, err := createStockItem(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{PartID: part.ID, LocationID: child.PK, Quantity: 4})
		r.NoError(err)
		a.Equal(StatusOK, created.Status)

		_, bare, err := getStockItem(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: created.Record.PK})
		r.NoError(err)
		a.Equal(StatusOK, bare.Status)
		a.Equal(created.Record.PK, bare.Record.PK)
		a.Nil(bare.Record.SKU)
		a.Nil(bare.Record.MPN)
		a.Nil(bare.Record.SalesOrder)
		a.Nil(bare.Record.SalesOrderReference)
		r.Len(bare.Record.LocationPath, 2)
		a.Equal(root.Name, bare.Record.LocationPath[0].Name)
		a.Equal(child.Name, bare.Record.LocationPath[1].Name)

		_, err = fixture.client.UpdateStockItem(ctx, created.Record.PK, inventree.PatchFields{"supplier_part": inventree.Set(supplierPart.ID), "expiry_date": inventree.Set("2020-01-01")})
		r.NoError(err)

		_, exact, err := getStockItem(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: created.Record.PK})
		r.NoError(err)
		a.Equal(StatusOK, exact.Status)
		r.NotNil(exact.Record.SKU)
		a.Equal(supplierPart.Name, *exact.Record.SKU)
		a.Nil(exact.Record.MPN)
		r.NotNil(exact.Record.Expired)
		a.True(*exact.Record.Expired)

		_, search, err := searchStockItems(fixture.deps())(ctx, &mcp.CallToolRequest{}, StockItemsInput{PartID: part.ID})
		r.NoError(err)
		a.Equal(StatusOK, search.Status)
		r.NotEmpty(search.Results)
		encoded, err := json.Marshal(search.Results[0])
		r.NoError(err)
		var keys map[string]any
		r.NoError(json.Unmarshal(encoded, &keys))
		a.NotContains(keys, "SKU")
		a.NotContains(keys, "MPN")
		a.NotContains(keys, "expired")
		a.NotContains(keys, "stale")
		a.NotContains(keys, "location_path")
		a.NotContains(keys, "sales_order_reference")
	})

	t.Run("owner_discovery_and_cross_object_responsibility_assignment", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		order := fixture.ensure(t, testenv.FixturePurchaseOrder)
		location := fixture.ensure(t, testenv.FixtureLocation)
		stockItem, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 1})
		r.NoError(err)

		user, err := fixture.client.GetCurrentUser(ctx)
		r.NoError(err)
		_, byQuery, err := searchOwners(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchOwnersInput{Query: user.Username})
		r.NoError(err)
		a.Equal(StatusOK, byQuery.Status)
		r.NotEmpty(byQuery.Results)
		ownerID := byQuery.Results[0].PK

		_, byObjectType, err := searchOwners(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchOwnersInput{ObjectType: OwnerObjectPart, Limit: 5})
		r.NoError(err)
		a.Equal(StatusOK, byObjectType.Status)
		r.NotEmpty(byObjectType.Results)

		_, _, err = searchOwners(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchOwnersInput{})
		require.Error(t, err)

		_, exactOwner, err := getOwner(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: ownerID})
		r.NoError(err)
		a.Equal(StatusOK, exactOwner.Status)
		a.Equal(ownerID, exactOwner.Record.PK)

		// is_active is a list-level query filter on InvenTree's Owner endpoint
		// (the Owner record itself carries no per-record active flag), so this
		// proves the filter reaches the live API and narrows results rather
		// than proving any single owner's active/disabled state.
		_, activeOnly, err := searchOwners(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchOwnersInput{Query: user.Username, IsActive: dvgoutils.Ptr(true)})
		r.NoError(err)
		a.Equal(StatusOK, activeOnly.Status)
		r.NotEmpty(activeOnly.Results)

		anotherLocation, err := fixture.run.Name("owner-discovery-location")
		r.NoError(err)
		_, anotherLocationOut, err := createStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateStockLocationInput{Name: anotherLocation})
		r.NoError(err)
		r.Equal(StatusOK, anotherLocationOut.Status)

		for _, tc := range []struct {
			objectType string
			objectID   int
			readBack   func() *int
		}{
			{objectType: OwnerObjectPart, objectID: part.ID, readBack: func() *int {
				_, out, getErr := getPart(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: part.ID})
				r.NoError(getErr)
				return out.Record.Responsible
			}},
			{objectType: OwnerObjectPurchaseOrder, objectID: order.ID, readBack: func() *int {
				_, out, getErr := getPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: order.ID})
				r.NoError(getErr)
				return out.Record.Responsible
			}},
			{objectType: OwnerObjectStockItem, objectID: stockItem.PK, readBack: func() *int {
				_, out, getErr := getStockItem(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: stockItem.PK})
				r.NoError(getErr)
				return out.Record.Owner
			}},
			{objectType: OwnerObjectStockLocation, objectID: anotherLocationOut.Record.PK, readBack: func() *int {
				_, out, getErr := getStockLocation(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: anotherLocationOut.Record.PK})
				r.NoError(getErr)
				return out.Record.Owner
			}},
		} {
			t.Run(tc.objectType, func(t *testing.T) {
				a := assert.New(t)
				r := require.New(t)
				a.Nil(tc.readBack())

				_, preview, assignErr := assignOwner(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: tc.objectType, ObjectID: tc.objectID, OwnerID: &ownerID})
				r.NoError(assignErr)
				r.Equal(StatusClarificationRequired, preview.Status)
				r.NotEmpty(preview.PlanHash)

				_, confirmed, assignErr := assignOwner(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: tc.objectType, ObjectID: tc.objectID, OwnerID: &ownerID, Confirm: true, PlanHash: preview.PlanHash})
				r.NoError(assignErr)
				r.Equal(StatusOK, confirmed.Status)
				a.True(confirmed.Verified)
				r.NotNil(tc.readBack())
				a.Equal(ownerID, *tc.readBack())

				_, clearPreview, assignErr := assignOwner(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: tc.objectType, ObjectID: tc.objectID})
				r.NoError(assignErr)
				r.Equal(StatusClarificationRequired, clearPreview.Status)
				_, cleared, assignErr := assignOwner(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignOwnerInput{ObjectType: tc.objectType, ObjectID: tc.objectID, Confirm: true, PlanHash: clearPreview.PlanHash})
				r.NoError(assignErr)
				r.Equal(StatusOK, cleared.Status)
				a.Nil(tc.readBack())
			})
		}
	})

	t.Run("structured_contact_and_address_references", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		order := fixture.ensure(t, testenv.FixturePurchaseOrder)
		otherCompany := fixture.ensure(t, testenv.FixtureManufacturer)

		createContact := func(companyID int, suffix string) inventree.Contact {
			name, nameErr := fixture.run.Name(suffix)
			r.NoError(nameErr)
			var contact inventree.Contact
			r.NoError(fixture.client.Post(ctx, "/api/company/contact/", map[string]any{"company": companyID, "name": name, "role": "Purchasing"}, &contact))
			return contact
		}
		createAddress := func(companyID int, suffix string) inventree.Address {
			title, titleErr := fixture.run.Name(suffix)
			r.NoError(titleErr)
			var address inventree.Address
			r.NoError(fixture.client.Post(ctx, "/api/company/address/", map[string]any{"company": companyID, "title": title, "line1": "1 Test Street", "postal_city": "Testville", "country": "AU"}, &address))
			return address
		}

		// contact2/address2 (a second same-company record used only to force plan
		// staleness below) are created later, right before they are needed: InvenTree's
		// contact/address "search" matches on shared run-scoped name prefixes rather
		// than strict substrings, so a second same-company record created up front
		// would make the single-result search assertions below ambiguous.
		contact := createContact(supplier.ID, "contact-alpha")
		otherContact := createContact(otherCompany.ID, "contact-external")
		address := createAddress(supplier.ID, "address-alpha")
		otherAddress := createAddress(otherCompany.ID, "address-external")

		_, contactSearch, err := searchContacts(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchContactsInput{CompanyID: supplier.ID, Search: contact.Name})
		r.NoError(err)
		a.Equal(StatusOK, contactSearch.Status)
		r.Len(contactSearch.Results, 1)
		a.Equal(contact.PK, contactSearch.Results[0].PK)
		a.Equal(supplier.ID, contactSearch.Results[0].Company)

		_, _, err = searchContacts(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchContactsInput{})
		a.Error(err)

		_, exactContact, err := getContact(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: contact.PK})
		r.NoError(err)
		a.Equal(StatusOK, exactContact.Status)
		a.Equal(contact.Name, exactContact.Record.Name)

		_, missingContact, err := getContact(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: 999999999})
		r.NoError(err)
		a.Equal(StatusNotFound, missingContact.Status)

		_, addressSearch, err := searchAddresses(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchAddressesInput{CompanyID: supplier.ID, Search: address.Title})
		r.NoError(err)
		a.Equal(StatusOK, addressSearch.Status)
		r.Len(addressSearch.Results, 1)
		a.Equal(address.PK, addressSearch.Results[0].PK)
		a.Equal(supplier.ID, addressSearch.Results[0].Company)

		_, _, err = searchAddresses(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchAddressesInput{})
		a.Error(err)

		_, exactAddress, err := getAddress(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: address.PK})
		r.NoError(err)
		a.Equal(StatusOK, exactAddress.Status)
		a.Equal(address.Title, exactAddress.Record.Title)

		_, missingAddress, err := getAddress(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: 999999999})
		r.NoError(err)
		a.Equal(StatusNotFound, missingAddress.Status)

		_, orderBefore, err := getPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: order.ID})
		r.NoError(err)
		a.Nil(orderBefore.Record.Contact)
		a.Nil(orderBefore.Record.Address)

		_, missingOrder, err := assignContact(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: 999999999, ContactID: &contact.PK})
		r.NoError(err)
		a.Equal(StatusNotFound, missingOrder.Status)

		_, contactMismatch, err := assignContact(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: order.ID, ContactID: &otherContact.PK})
		r.NoError(err)
		a.Equal(StatusValidationFailed, contactMismatch.Status)

		_, contactPreview, err := assignContact(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: order.ID, ContactID: &contact.PK})
		r.NoError(err)
		r.Equal(StatusClarificationRequired, contactPreview.Status)
		r.NotEmpty(contactPreview.PlanHash)

		_, contactConfirmed, err := assignContact(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: order.ID, ContactID: &contact.PK, Confirm: true, PlanHash: contactPreview.PlanHash})
		r.NoError(err)
		r.Equal(StatusOK, contactConfirmed.Status)
		a.True(contactConfirmed.Verified)
		r.NotNil(contactConfirmed.ContactID)
		a.Equal(contact.PK, *contactConfirmed.ContactID)

		_, contactClearPreview, err := assignContact(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: order.ID})
		r.NoError(err)
		r.Equal(StatusClarificationRequired, contactClearPreview.Status)
		contact2 := createContact(supplier.ID, "contact-bravo")
		_, err = fixture.client.UpdatePurchaseOrderDetail(ctx, order.ID, inventree.PatchFields{"contact": inventree.Set(contact2.PK)})
		r.NoError(err)
		_, staleContactClear, err := assignContact(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: order.ID, Confirm: true, PlanHash: contactClearPreview.PlanHash})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, staleContactClear.Status)

		_, freshContactClearPreview, err := assignContact(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: order.ID})
		r.NoError(err)
		r.Equal(StatusClarificationRequired, freshContactClearPreview.Status)
		_, contactCleared, err := assignContact(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignContactInput{PurchaseOrderID: order.ID, Confirm: true, PlanHash: freshContactClearPreview.PlanHash})
		r.NoError(err)
		r.Equal(StatusOK, contactCleared.Status)
		a.Nil(contactCleared.ContactID)

		_, addressMismatch, err := assignAddress(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: order.ID, AddressID: &otherAddress.PK})
		r.NoError(err)
		a.Equal(StatusValidationFailed, addressMismatch.Status)

		_, addressPreview, err := assignAddress(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: order.ID, AddressID: &address.PK})
		r.NoError(err)
		r.Equal(StatusClarificationRequired, addressPreview.Status)
		r.NotEmpty(addressPreview.PlanHash)

		_, addressConfirmed, err := assignAddress(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: order.ID, AddressID: &address.PK, Confirm: true, PlanHash: addressPreview.PlanHash})
		r.NoError(err)
		r.Equal(StatusOK, addressConfirmed.Status)
		a.True(addressConfirmed.Verified)
		r.NotNil(addressConfirmed.AddressID)
		a.Equal(address.PK, *addressConfirmed.AddressID)

		_, addressClearPreview, err := assignAddress(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: order.ID})
		r.NoError(err)
		r.Equal(StatusClarificationRequired, addressClearPreview.Status)
		address2 := createAddress(supplier.ID, "address-bravo")
		_, err = fixture.client.UpdatePurchaseOrderDetail(ctx, order.ID, inventree.PatchFields{"address": inventree.Set(address2.PK)})
		r.NoError(err)
		_, staleAddressClear, err := assignAddress(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: order.ID, Confirm: true, PlanHash: addressClearPreview.PlanHash})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, staleAddressClear.Status)

		_, freshAddressClearPreview, err := assignAddress(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: order.ID})
		r.NoError(err)
		r.Equal(StatusClarificationRequired, freshAddressClearPreview.Status)
		_, addressCleared, err := assignAddress(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignAddressInput{PurchaseOrderID: order.ID, Confirm: true, PlanHash: freshAddressClearPreview.PlanHash})
		r.NoError(err)
		r.Equal(StatusOK, addressCleared.Status)
		a.Nil(addressCleared.AddressID)

		_, orderAfter, err := getPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: order.ID})
		r.NoError(err)
		a.Nil(orderAfter.Record.Contact)
		a.Nil(orderAfter.Record.Address)
	})

	t.Run("project_code_discovery_and_assignment", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		destination := fixture.ensure(t, testenv.FixtureLocation)

		createProjectCode := func(suffix string) inventree.ProjectCode {
			code, codeErr := fixture.run.Name(suffix)
			r.NoError(codeErr)
			var record inventree.ProjectCode
			r.NoError(fixture.client.Post(ctx, "/api/project-code/", map[string]any{"code": code, "description": "F-S50 integration", "active": true}, &record))
			return record
		}
		projectCode := createProjectCode("prj-alpha")

		order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID})
		r.NoError(err)
		lineReference, err := fixture.run.Name("po-line")
		r.NoError(err)
		line, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &lineReference, Quantity: 1, Destination: &destination.ID})
		r.NoError(err)
		extraReference, err := fixture.run.Name("po-extra-line")
		r.NoError(err)
		extraLine, err := fixture.client.CreatePurchaseOrderExtraLine(ctx, inventree.PurchaseOrderExtraLineCreate{Order: order.PK, Reference: extraReference, Quantity: 1})
		r.NoError(err)

		_, projectCodeSearch, err := searchProjectCodes(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchProjectCodesInput{Query: projectCode.Code})
		r.NoError(err)
		a.Equal(StatusOK, projectCodeSearch.Status)
		r.Len(projectCodeSearch.Results, 1)
		a.Equal(projectCode.PK, projectCodeSearch.Results[0].PK)

		_, exactProjectCode, err := getProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: projectCode.PK})
		r.NoError(err)
		a.Equal(StatusOK, exactProjectCode.Status)
		a.Equal(projectCode.Code, exactProjectCode.Record.Code)

		_, missingProjectCode, err := getProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: 999999999})
		r.NoError(err)
		a.Equal(StatusNotFound, missingProjectCode.Status)

		_, orderBefore, err := getPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: order.PK})
		r.NoError(err)
		a.Nil(orderBefore.Record.ProjectCode)

		cases := []struct {
			name       string
			shortCode  string
			objectType string
			objectID   int
		}{
			{name: "purchase_order", shortCode: "po", objectType: ProjectCodeObjectPurchaseOrder, objectID: order.PK},
			{name: "purchase_order_line", shortCode: "pl", objectType: ProjectCodeObjectPurchaseOrderLine, objectID: line.PK},
			{name: "purchase_order_extra_line", shortCode: "pe", objectType: ProjectCodeObjectPurchaseOrderExtraLine, objectID: extraLine.PK},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				r := require.New(t)
				a := assert.New(t)

				_, preview, err := assignProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: tc.objectType, ObjectID: tc.objectID, ProjectCodeID: &projectCode.PK})
				r.NoError(err)
				r.Equal(StatusClarificationRequired, preview.Status)
				r.NotEmpty(preview.PlanHash)

				_, confirmed, err := assignProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: tc.objectType, ObjectID: tc.objectID, ProjectCodeID: &projectCode.PK, Confirm: true, PlanHash: preview.PlanHash})
				r.NoError(err)
				r.Equal(StatusOK, confirmed.Status)
				a.True(confirmed.Verified)
				r.NotNil(confirmed.ProjectCodeID)
				a.Equal(projectCode.PK, *confirmed.ProjectCodeID)

				_, clearPreview, err := assignProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: tc.objectType, ObjectID: tc.objectID})
				r.NoError(err)
				r.Equal(StatusClarificationRequired, clearPreview.Status)

				// The object's project code changes before confirmation, so the stale plan must be refused.
				otherProjectCode := createProjectCode(tc.shortCode + "-alt")
				_, restale, err := assignProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: tc.objectType, ObjectID: tc.objectID, ProjectCodeID: &otherProjectCode.PK, Confirm: true, PlanHash: clearPreview.PlanHash})
				r.NoError(err)
				a.Equal(StatusClarificationRequired, restale.Status)

				_, freshClearPreview, err := assignProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: tc.objectType, ObjectID: tc.objectID})
				r.NoError(err)
				r.Equal(StatusClarificationRequired, freshClearPreview.Status)
				_, cleared, err := assignProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: tc.objectType, ObjectID: tc.objectID, Confirm: true, PlanHash: freshClearPreview.PlanHash})
				r.NoError(err)
				r.Equal(StatusOK, cleared.Status)
				a.Nil(cleared.ProjectCodeID)
			})
		}

		// Confirm the combined-workflow preservation contract end-to-end: assign a
		// project code to the line, then prove issue_purchase_order's dry-run
		// carries it into the plan/hash and rejects a stale reassignment.
		_, linePreview, err := assignProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderLine, ObjectID: line.PK, ProjectCodeID: &projectCode.PK})
		r.NoError(err)
		_, lineConfirmed, err := assignProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderLine, ObjectID: line.PK, ProjectCodeID: &projectCode.PK, Confirm: true, PlanHash: linePreview.PlanHash})
		r.NoError(err)
		r.Equal(StatusOK, lineConfirmed.Status)

		_, issuePlan, err := issuePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{DryRun: true, OrderID: order.PK})
		r.NoError(err)
		r.NotEmpty(issuePlan.PlanHash)
		lineWithProjectCode, ok := findPurchaseOrderLine(issuePlan.Lines, line.PK)
		r.True(ok)
		r.NotNil(lineWithProjectCode.ProjectCode)
		a.Equal(projectCode.PK, *lineWithProjectCode.ProjectCode)

		otherProjectCode := createProjectCode("workflow-drift")
		_, driftPreview, err := assignProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderLine, ObjectID: line.PK, ProjectCodeID: &otherProjectCode.PK})
		r.NoError(err)
		_, driftConfirmed, err := assignProjectCode(fixture.deps())(ctx, &mcp.CallToolRequest{}, AssignProjectCodeInput{ObjectType: ProjectCodeObjectPurchaseOrderLine, ObjectID: line.PK, ProjectCodeID: &otherProjectCode.PK, Confirm: true, PlanHash: driftPreview.PlanHash})
		r.NoError(err)
		r.Equal(StatusOK, driftConfirmed.Status)

		// The reviewed issue_purchase_order plan hash bound the line's prior
		// project code, so it must now be stale.
		_, staleIssue, err := issuePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{OrderID: order.PK, ConfirmIssue: true, PlanHash: issuePlan.PlanHash})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, staleIssue.Status)
	})

	t.Run("company_and_sourcing_link_administration", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		manufacturer := fixture.ensure(t, testenv.FixtureManufacturer)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		manufacturerPart := fixture.createManufacturerPart(t, part.ID, manufacturer.ID)

		_, companyOut, err := getCompanyAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: supplier.ID})
		r.NoError(err)
		r.NotNil(companyOut.Record)
		a.True(companyOut.Record.IsSupplier)

		description := "updated through F-S20 tool integration"
		_, companyUpdate, err := updateCompanyAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: supplier.ID, Description: &description})
		r.NoError(err)
		a.Equal(StatusOK, companyUpdate.Status)
		r.NotNil(companyUpdate.Record)
		a.Equal(description, companyUpdate.Record.Description)

		removeSupplier := false
		_, blockedRemoval, err := updateCompanyAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: supplier.ID, IsSupplier: &removeSupplier, Confirm: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, blockedRemoval.Status)

		_, supplierSearch, err := searchSupplierPartsAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, SupplierPartSearchInput{PartID: part.ID, SupplierID: supplier.ID, SKU: supplierPart.Name})
		r.NoError(err)
		r.NotEmpty(supplierSearch.Results)
		_, supplierGet, err := getSupplierPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: supplierPart.ID})
		r.NoError(err)
		r.NotNil(supplierGet.Record)
		packaging := "reel"
		supplierLongNotes := "supplier **Markdown** notes"
		explicitZeroAvailability := 0.0
		_, supplierUpdate, err := updateSupplierPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: supplierPart.ID, Packaging: &packaging, ManufacturerPartID: &manufacturerPart.PK, Notes: &supplierLongNotes, Available: &explicitZeroAvailability})
		r.NoError(err)
		a.Equal(StatusOK, supplierUpdate.Status)
		r.NotNil(supplierUpdate.Record)
		r.NotNil(supplierUpdate.Record.Packaging)
		a.Equal(packaging, *supplierUpdate.Record.Packaging)
		a.Equal(supplierLongNotes, *supplierUpdate.Record.Notes)
		a.Zero(supplierUpdate.Record.Available)
		a.Equal(manufacturerPart.MPN, *supplierUpdate.Record.MPN)
		r.NotNil(supplierUpdate.Record.AvailabilityUpdated)
		r.NotNil(supplierUpdate.Record.InStock)
		r.NotNil(supplierUpdate.Record.OnOrder)
		r.NotNil(supplierUpdate.Record.Updated)

		_, manufacturerSearch, err := searchManufacturerPartsAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, ManufacturerPartSearchInput{PartID: part.ID, ManufacturerID: manufacturer.ID, MPN: manufacturerPart.MPN})
		r.NoError(err)
		r.NotEmpty(manufacturerSearch.Results)
		_, manufacturerGet, err := getManufacturerPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: manufacturerPart.PK})
		r.NoError(err)
		r.NotNil(manufacturerGet.Record)
		manufacturerDescription := "verified F-S20 manufacturer link"
		manufacturerLongNotes := "manufacturer **Markdown** notes"
		_, manufacturerUpdate, err := updateManufacturerPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: manufacturerPart.PK, Description: &manufacturerDescription, Notes: &manufacturerLongNotes})
		r.NoError(err)
		a.Equal(StatusOK, manufacturerUpdate.Status)
		r.NotNil(manufacturerUpdate.Record)
		r.NotNil(manufacturerUpdate.Record.Description)
		a.Equal(manufacturerDescription, *manufacturerUpdate.Record.Description)
		a.Equal(manufacturerLongNotes, *manufacturerUpdate.Record.Notes)

		missingPartID := 999999999
		_, _, err = updateSupplierPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: supplierPart.ID, PartID: &missingPartID})
		a.Error(err)
		manufacturerAsSupplier := manufacturer.ID
		_, _, err = updateSupplierPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: supplierPart.ID, SupplierID: &manufacturerAsSupplier})
		a.Error(err)
		_, _, err = updateManufacturerPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: manufacturerPart.PK, PartID: &missingPartID})
		a.Error(err)
		supplierAsManufacturer := supplier.ID
		_, _, err = updateManufacturerPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: manufacturerPart.PK, ManufacturerID: &supplierAsManufacturer})
		a.Error(err)
		otherPart := fixture.createPart(t, "sourcing-other-part")
		wrongBaseMPN, err := fixture.run.Name("wrong-base-manufacturer-part")
		r.NoError(err)
		wrongBaseManufacturerPart, err := fixture.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{Part: otherPart.PK, Manufacturer: manufacturer.ID, MPN: dvgoutils.Ptr(wrongBaseMPN)})
		r.NoError(err)
		_, _, err = updateSupplierPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: supplierPart.ID, ManufacturerPartID: &wrongBaseManufacturerPart.PK})
		a.Error(err)

		duplicateSKU, err := fixture.run.Name("supplierpart-duplicate-source")
		r.NoError(err)
		secondSupplierPart, err := fixture.client.CreateSupplierPart(ctx, inventree.SupplierPartCreate{Part: part.ID, Supplier: supplier.ID, SKU: duplicateSKU})
		r.NoError(err)
		targetSKU := supplierPart.Name
		_, supplierDuplicate, err := updateSupplierPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: secondSupplierPart.PK, SKU: &targetSKU})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, supplierDuplicate.Status)
		r.NotEmpty(supplierDuplicate.Candidates)

		secondMPN, err := fixture.run.Name("mfgpart-duplicate-source")
		r.NoError(err)
		secondManufacturerPart, err := fixture.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{Part: part.ID, Manufacturer: manufacturer.ID, MPN: dvgoutils.Ptr(secondMPN)})
		r.NoError(err)
		targetMPN := manufacturerPart.MPN
		_, manufacturerDuplicate, err := updateManufacturerPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: secondManufacturerPart.PK, MPN: &targetMPN})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, manufacturerDuplicate.Status)
		r.NotEmpty(manufacturerDuplicate.Candidates)

		empty := ""
		inactive := false
		temporaryNote := "temporary note cleared by F-S20 integration"
		_, explicitSupplierValues, err := updateSupplierPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: supplierPart.ID, Description: &empty, Active: &inactive, Note: &temporaryNote})
		r.NoError(err)
		a.Equal(StatusOK, explicitSupplierValues.Status)
		r.NotNil(explicitSupplierValues.Record)
		r.NotNil(explicitSupplierValues.Record.Description)
		a.Empty(*explicitSupplierValues.Record.Description)
		a.False(explicitSupplierValues.Record.Active)
		_, clearedSupplierNote, err := updateSupplierPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateSupplierPartInput{ID: supplierPart.ID, ClearNote: true, ClearNotes: true})
		r.NoError(err)
		a.Equal(StatusOK, clearedSupplierNote.Status)
		r.NotNil(clearedSupplierNote.Record)
		a.Nil(clearedSupplierNote.Record.Note)
		a.Nil(clearedSupplierNote.Record.Notes)

		_, explicitManufacturerValues, err := updateManufacturerPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: manufacturerPart.PK, Description: &empty, Link: &empty})
		r.NoError(err)
		a.Equal(StatusOK, explicitManufacturerValues.Status)
		r.NotNil(explicitManufacturerValues.Record)
		r.NotNil(explicitManufacturerValues.Record.Description)
		a.Empty(*explicitManufacturerValues.Record.Description)
		_, clearedManufacturerValues, err := updateManufacturerPartAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateManufacturerPartInput{ID: manufacturerPart.PK, ClearDescription: true, ClearLink: true, ClearNotes: true})
		r.NoError(err)
		a.Equal(StatusOK, clearedManufacturerValues.Status)
		r.NotNil(clearedManufacturerValues.Record)
		a.Nil(clearedManufacturerValues.Record.Description)
		a.Empty(clearedManufacturerValues.Record.Link)
		a.Nil(clearedManufacturerValues.Record.Notes)

		roleRemovalName, err := fixture.run.Name("supplier-role-removal")
		r.NoError(err)
		roleRemovalCompany, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{Name: roleRemovalName, Currency: "USD", IsSupplier: true})
		r.NoError(err)
		removeRole := false
		_, roleRemoval, err := updateCompanyAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: roleRemovalCompany.PK, IsSupplier: &removeRole, Confirm: true})
		r.NoError(err)
		a.Equal(StatusOK, roleRemoval.Status)
		r.NotNil(roleRemoval.Record)
		a.False(roleRemoval.Record.IsSupplier)

		manufacturerRoleRemovalName, err := fixture.run.Name("manufacturer-role-removal")
		r.NoError(err)
		manufacturerRoleRemovalCompany, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{Name: manufacturerRoleRemovalName, Currency: "USD", IsManufacturer: true})
		r.NoError(err)
		_, manufacturerRoleRemoval, err := updateCompanyAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: manufacturerRoleRemovalCompany.PK, IsManufacturer: &removeRole, Confirm: true})
		r.NoError(err)
		a.Equal(StatusOK, manufacturerRoleRemoval.Status)
		r.NotNil(manufacturerRoleRemoval.Record)
		a.False(manufacturerRoleRemoval.Record.IsManufacturer)

		companyNote := "temporary company note"
		_, explicitCompanyValues, err := updateCompanyAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: roleRemovalCompany.PK, Notes: &companyNote})
		r.NoError(err)
		a.Equal(StatusOK, explicitCompanyValues.Status)
		r.NotNil(explicitCompanyValues.Record)
		r.NotNil(explicitCompanyValues.Record.Notes)
		a.Equal(companyNote, *explicitCompanyValues.Record.Notes)
		_, clearedCompanyNote, err := updateCompanyAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: roleRemovalCompany.PK, ClearNotes: true})
		r.NoError(err)
		a.Equal(StatusOK, clearedCompanyNote.Status)
		r.NotNil(clearedCompanyNote.Record)
		a.Nil(clearedCompanyNote.Record.Notes)
	})

	t.Run("company_customer_role_removal_happy_path", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		location := fixture.ensure(t, testenv.FixtureLocation)
		part := fixture.ensure(t, testenv.FixturePart)

		customerName, err := fixture.run.Name("customer-role-removal")
		r.NoError(err)
		created, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{Name: customerName, Currency: "USD"})
		r.NoError(err)
		add := true
		_, addRole, err := updateCompanyAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: created.PK, IsCustomer: &add})
		r.NoError(err)
		a.Equal(StatusOK, addRole.Status)
		r.NotNil(addRole.Record)
		a.True(addRole.Record.IsCustomer)

		remove := false
		_, _, err = updateCompanyAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateCompanyInput{ID: created.PK, IsCustomer: &remove})
		a.Error(err, "update_company must always reject inline customer-role removal")

		// Dependency-blocked: a stock item assigned to this customer must
		// refuse removal and issue no plan token, then allow removal once
		// the dependency clears.
		stockItem, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 1})
		r.NoError(err)
		_, err = fixture.client.UpdateStockItem(ctx, stockItem.PK, inventree.PatchFields{"customer": inventree.Set(created.PK)})
		r.NoError(err)

		_, blockedPreview, err := removeCompanyCustomerRole(fixture.deps())(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: created.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, blockedPreview.Status)
		a.Equal(1, blockedPreview.DependencyStockItems)
		a.Empty(blockedPreview.PlanHash, "no plan token is issued while a dependency remains")

		_, err = fixture.client.UpdateStockItem(ctx, stockItem.PK, inventree.PatchFields{"customer": inventree.Null()})
		r.NoError(err)

		_, preview, err := removeCompanyCustomerRole(fixture.deps())(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: created.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, preview.Status)
		r.NotEmpty(preview.PlanHash)
		r.NotNil(preview.Plan)
		a.Equal(created.PK, preview.Plan.CompanyID)

		_, confirmed, err := removeCompanyCustomerRole(fixture.deps())(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: created.PK, Confirm: true, PlanHash: preview.PlanHash})
		r.NoError(err)
		a.Equal(StatusOK, confirmed.Status)
		a.True(confirmed.Verified)
		r.NotNil(confirmed.Record)
		a.False(confirmed.Record.IsCustomer)

		_, alreadyRemoved, err := removeCompanyCustomerRole(fixture.deps())(ctx, &mcp.CallToolRequest{}, RemoveCompanyCustomerRoleInput{CompanyID: created.PK})
		r.NoError(err)
		a.Equal(StatusValidationFailed, alreadyRemoved.Status)
	})

	t.Run("purchase_order_create_and_retry_happy_path", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		destination := fixture.ensure(t, testenv.FixtureLocation)
		supplierReference, err := fixture.run.Name("order-page")
		r.NoError(err)
		price := 1.25
		extraReference, err := fixture.run.Name("order-extra")
		r.NoError(err)
		input := PurchaseOrderWorkflowInput{
			SupplierID:        supplier.ID,
			SupplierReference: supplierReference,
			Description:       dvgoutils.Ptr("order-page integration workflow"),
			Currency:          dvgoutils.Ptr("AUD"),
			Lines: []PurchaseOrderWorkflowLine{{
				SupplierPartID: supplierPart.ID,
				Quantity:       4,
				UnitPrice:      &price,
				Currency:       "AUD",
				Notes:          "created by F-S03 integration coverage",
				DestinationID:  &destination.ID,
			}, {
				SupplierPartID: supplierPart.ID,
				Quantity:       2,
				UnitPrice:      &price,
				Currency:       "AUD",
				Notes:          "separate same-part line must not merge",
				DestinationID:  &destination.ID,
			}},
			ExtraLines: []PurchaseOrderWorkflowExtraLine{{Reference: extraReference, Description: dvgoutils.Ptr("original supplier invoice line"), Quantity: 1, UnitPrice: dvgoutils.Ptr("0"), Currency: dvgoutils.Ptr("AUD")}},
		}
		dryRunInput := input
		dryRunInput.DryRun = true
		_, dryRun, err := createPurchaseOrderWithLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, dryRunInput)
		r.NoError(err)
		a.Equal(StatusOK, dryRun.Status)
		r.Len(dryRun.PlannedChanges, 4)
		a.Equal("create_purchase_order_extra_line", dryRun.PlannedChanges[3].Action)

		_, created, err := createPurchaseOrderWithLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusOK, created.Status)
		r.NotNil(created.PurchaseOrder)
		r.Len(created.Lines, 2)
		r.Len(created.ExtraLines, 1)
		a.NotEmpty(created.PurchaseOrder.Reference)
		a.Equal(supplierReference, created.PurchaseOrder.SupplierReference)
		a.Equal(supplierReference+"-1", created.Lines[0].Reference)
		a.Equal(supplierReference+"-2", created.Lines[1].Reference)
		a.NotEqual(created.Lines[0].PK, created.Lines[1].PK)
		r.NotNil(created.Lines[0].Destination)
		r.NotNil(created.Lines[1].Destination)
		a.Equal(destination.ID, *created.Lines[0].Destination)
		a.Equal(destination.ID, *created.Lines[1].Destination)
		a.Equal(extraReference, created.ExtraLines[0].Reference)
		r.NotNil(created.ExtraLines[0].Price)
		a.True(decimalPointerMatches(dvgoutils.Ptr("0"), created.ExtraLines[0].Price))
		r.NotNil(created.PurchaseOrder.TotalPrice)

		input.Description = dvgoutils.Ptr("updated by retry recovery")
		input.Lines[0].Quantity = 5
		input.ExtraLines[0].Notes = dvgoutils.Ptr("retained invoice context")
		_, retried, err := createPurchaseOrderWithLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusOK, retried.Status)
		r.NotNil(retried.PurchaseOrder)
		r.Len(retried.Lines, 2)
		r.Len(retried.ExtraLines, 1)
		a.Equal(created.PurchaseOrder.PK, retried.PurchaseOrder.PK)
		a.Equal(created.Lines[0].PK, retried.Lines[0].PK)
		a.Equal(created.Lines[1].PK, retried.Lines[1].PK)
		a.Equal(created.ExtraLines[0].PK, retried.ExtraLines[0].PK)
		a.Equal("retained invoice context", retried.ExtraLines[0].Notes)
		a.Equal(5.0, retried.Lines[0].Quantity)

		deleteReference, err := fixture.run.Name("delete-extra")
		r.NoError(err)
		_, extraToDelete, err := createPurchaseOrderExtraLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePurchaseOrderExtraLineInput{OrderID: created.PurchaseOrder.PK, Reference: deleteReference, Quantity: 1, UnitPrice: dvgoutils.Ptr("-0.25"), Currency: dvgoutils.Ptr("AUD")})
		r.NoError(err)
		r.NotNil(extraToDelete.Record)
		r.NotNil(extraToDelete.PurchaseOrder)
		r.NotNil(extraToDelete.PurchaseOrder.TotalPrice)
		r.NotNil(retried.PurchaseOrder.TotalPrice)
		beforeDiscount, ok := new(big.Rat).SetString(string(*retried.PurchaseOrder.TotalPrice))
		r.True(ok)
		afterDiscount, ok := new(big.Rat).SetString(string(*extraToDelete.PurchaseOrder.TotalPrice))
		r.True(ok)
		r.Zero(new(big.Rat).Sub(afterDiscount, beforeDiscount).Cmp(big.NewRat(-1, 4)), "standalone tool must return the exact -0.25 total effect")
		_, deletePreview, err := deletePurchaseOrderExtraLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderExtraLineInput{ID: extraToDelete.Record.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, deletePreview.Status)
		_, deletedExtra, err := deletePurchaseOrderExtraLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderExtraLineInput{ID: extraToDelete.Record.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusOK, deletedExtra.Status)
		a.True(deletedExtra.Verified)
		r.NotNil(deletedExtra.PurchaseOrder)
		r.NotNil(deletedExtra.PurchaseOrder.TotalPrice)
		a.Equal(*retried.PurchaseOrder.TotalPrice, *deletedExtra.PurchaseOrder.TotalPrice, "confirmed delete must return the refreshed restored total")

		orders, err := fixture.client.SearchPurchaseOrders(ctx, inventree.PurchaseOrderQuery{Search: supplierReference, Supplier: supplier.ID})
		r.NoError(err)
		exactOrders := exactSupplierReferenceMatches(orders, supplier.ID, supplierReference)
		r.Len(exactOrders, 1)
		lines, err := fixture.client.SearchPurchaseOrderLines(ctx, inventree.PurchaseOrderLineQuery{Order: exactOrders[0].PK})
		r.NoError(err)
		r.Len(lines, 2)

		_, issuePlan, err := issuePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{DryRun: true, OrderID: exactOrders[0].PK})
		r.NoError(err)
		a.Equal(StatusOK, issuePlan.Status)
		a.Equal("issue_purchase_order", issuePlan.Action)
		r.NotEmpty(issuePlan.PlanHash)
		r.Len(issuePlan.Lines, 2)
		_, issued, err := issuePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, IssuePurchaseOrderInput{OrderID: exactOrders[0].PK, ConfirmIssue: true, PlanHash: issuePlan.PlanHash})
		r.NoError(err)
		r.NotNil(issued.Order)
		a.Equal(inventree.PurchaseOrderStatusPlaced, issued.Order.Status)

		receiveInput := ReceivePurchaseOrderInput{DryRun: true, OrderID: exactOrders[0].PK, Items: []ReceivePurchaseOrderItem{{LineItemID: lines[0].PK, Quantity: 2, BatchCode: dvgoutils.Ptr("receipt-a")}, {LineItemID: lines[1].PK, Quantity: 1, BatchCode: dvgoutils.Ptr("receipt-b")}}}
		_, receivePlan, err := receivePurchaseOrderItems(fixture.deps())(ctx, &mcp.CallToolRequest{}, receiveInput)
		r.NoError(err)
		a.Equal(StatusOK, receivePlan.Status)
		r.Len(receivePlan.Plan, 2)
		a.Equal(destination.ID, receivePlan.Plan[0].LocationID)
		a.Equal(destination.ID, receivePlan.Plan[1].LocationID)
		receiveInput.DryRun = false
		receiveInput.ConfirmReceive = true
		receiveInput.PlanHash = receivePlan.PlanHash
		_, received, err := receivePurchaseOrderItems(fixture.deps())(ctx, &mcp.CallToolRequest{}, receiveInput)
		r.NoError(err)
		a.Equal(StatusOK, received.Status)
		r.Len(received.StockItems, 2)
		a.NotEqual(received.StockItems[0].PK, received.StockItems[1].PK)
		orderStock, err := fixture.client.SearchStockItems(ctx, inventree.StockItemQuery{PurchaseOrderID: exactOrders[0].PK})
		r.NoError(err)
		r.Len(orderStock, 2)
		for _, stockItem := range orderStock {
			r.NotNil(stockItem.PurchaseOrder)
			a.Equal(exactOrders[0].PK, *stockItem.PurchaseOrder)
		}
		linesAfterReceipt, err := fixture.client.SearchPurchaseOrderLines(ctx, inventree.PurchaseOrderLineQuery{Order: exactOrders[0].PK})
		r.NoError(err)
		r.Len(linesAfterReceipt, 2)
		receivedByLine := map[int]float64{}
		for _, line := range linesAfterReceipt {
			receivedByLine[line.PK] = line.Received
		}
		a.Equal(2.0, receivedByLine[lines[0].PK])
		a.Equal(1.0, receivedByLine[lines[1].PK])

		completeInput := ReceivePurchaseOrderInput{DryRun: true, OrderID: exactOrders[0].PK, Items: []ReceivePurchaseOrderItem{{LineItemID: lines[0].PK, Quantity: 3, BatchCode: dvgoutils.Ptr("receipt-c")}, {LineItemID: lines[1].PK, Quantity: 1, BatchCode: dvgoutils.Ptr("receipt-d")}}}
		_, completePlan, err := receivePurchaseOrderItems(fixture.deps())(ctx, &mcp.CallToolRequest{}, completeInput)
		r.NoError(err)
		r.NotEmpty(completePlan.PlanHash)
		completeInput.DryRun = false
		completeInput.ConfirmReceive = true
		completeInput.PlanHash = completePlan.PlanHash
		_, completed, err := receivePurchaseOrderItems(fixture.deps())(ctx, &mcp.CallToolRequest{}, completeInput)
		r.NoError(err)
		a.Equal(StatusOK, completed.Status)
		r.NotNil(completed.Order)
		a.Equal(inventree.PurchaseOrderStatusComplete, completed.Order.Status)
		r.Len(completed.StockItems, 2)
	})

	t.Run("purchase_order_and_line_detail_completeness", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		destination := fixture.ensure(t, testenv.FixtureLocation)

		_, created, err := createPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePurchaseOrderInput{SupplierID: supplier.ID, DestinationID: &destination.ID})
		r.NoError(err)
		a.Equal(StatusOK, created.Status)
		orderID := created.Record.PK

		lineReference, err := fixture.run.Name("detail-line")
		r.NoError(err)
		price := 2.5
		_, addedLine, err := addPurchaseOrderLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, AddPurchaseOrderLineInput{OrderID: orderID, SupplierPartID: supplierPart.ID, Reference: &lineReference, Quantity: 3, UnitPrice: &price, Currency: dvgoutils.Ptr("AUD")})
		r.NoError(err)
		a.Equal(StatusOK, addedLine.Status)
		lineID := addedLine.Record.PK

		_, gotOrder, err := getPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: orderID})
		r.NoError(err)
		a.Equal(StatusOK, gotOrder.Status)
		a.Equal(orderID, gotOrder.Record.PK)
		a.NotZero(gotOrder.Record.CreatedBy.PK)
		r.NotNil(gotOrder.Record.LineItems)
		a.Equal(1, *gotOrder.Record.LineItems)
		r.NotNil(gotOrder.Record.StatusText)

		_, gotLine, err := getPurchaseOrderLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: lineID})
		r.NoError(err)
		a.Equal(StatusOK, gotLine.Status)
		a.Equal(lineID, gotLine.Record.PK)
		r.NotNil(gotLine.Record.SKU)
		a.Equal(supplierPart.Name, *gotLine.Record.SKU)
		r.NotNil(gotLine.Record.TotalPrice)
		a.Nil(gotLine.Record.BuildOrder)

		orderLink := "https://example.com/detail/" + lineReference
		_, updatedOrder, err := updatePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePurchaseOrderInput{ID: orderID, Description: dvgoutils.Ptr("updated through update_purchase_order"), Link: &orderLink})
		r.NoError(err)
		a.Equal(StatusOK, updatedOrder.Status)
		a.Equal("updated through update_purchase_order", updatedOrder.Record.Description)
		a.Equal(orderLink, updatedOrder.Record.Link)

		_, emptyPatch, err := updatePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePurchaseOrderInput{ID: orderID})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, emptyPatch.Status)
		r.NotNil(emptyPatch.Clarification)

		lineLink := "https://example.com/detail/line/" + lineReference
		_, updatedLine, err := updatePurchaseOrderLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdatePurchaseOrderLineInput{ID: lineID, Link: &lineLink, Discount: dvgoutils.Ptr(7.5)})
		r.NoError(err)
		a.Equal(StatusOK, updatedLine.Status)
		a.Equal(lineLink, updatedLine.Record.Link)
		a.Equal(7.5, updatedLine.Record.Discount)

		_, gotLineAfterUpdate, err := getPurchaseOrderLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, IDInput{ID: lineID})
		r.NoError(err)
		a.Equal(lineLink, gotLineAfterUpdate.Record.Link)
		a.Equal(7.5, gotLineAfterUpdate.Record.Discount)
	})

	t.Run("purchase_order_completion_with_auto_complete_disabled", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		destination := fixture.ensure(t, testenv.FixtureLocation)

		originalAutoComplete := purchaseOrderAutoCompleteSetting(t, ctx, fixture.client)
		setPurchaseOrderAutoComplete(t, ctx, fixture.client, false)
		t.Cleanup(func() {
			cleanupCtx := context.WithoutCancel(ctx)
			setPurchaseOrderAutoComplete(t, cleanupCtx, fixture.client, originalAutoComplete)
			r.Equal(originalAutoComplete, purchaseOrderAutoCompleteSetting(t, cleanupCtx, fixture.client))
		})

		createFullyReceivableOrder := func(suffix string) (inventree.PurchaseOrder, []inventree.PurchaseOrderLineItem) {
			t.Helper()
			order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID})
			r.NoError(err)
			firstReference, err := fixture.run.Name(suffix + "-one")
			r.NoError(err)
			secondReference, err := fixture.run.Name(suffix + "-two")
			r.NoError(err)
			first, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &firstReference, Quantity: 2, Destination: &destination.ID})
			r.NoError(err)
			second, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &secondReference, Quantity: 1, Destination: &destination.ID})
			r.NoError(err)
			r.NoError(fixture.client.IssuePurchaseOrder(ctx, order.PK))
			return order, []inventree.PurchaseOrderLineItem{first, second}
		}

		deferredOrder, deferredLines := createFullyReceivableOrder("po-deferred-completion")
		deferredReceipt := ReceivePurchaseOrderInput{DryRun: true, OrderID: deferredOrder.PK, Items: []ReceivePurchaseOrderItem{{LineItemID: deferredLines[0].PK, Quantity: 2}, {LineItemID: deferredLines[1].PK, Quantity: 1}}}
		_, deferredPlan, err := receivePurchaseOrderItems(fixture.deps())(ctx, &mcp.CallToolRequest{}, deferredReceipt)
		r.NoError(err)
		deferredReceipt.DryRun = false
		deferredReceipt.ConfirmReceive = true
		deferredReceipt.PlanHash = deferredPlan.PlanHash
		_, deferredResult, err := receivePurchaseOrderItems(fixture.deps())(ctx, &mcp.CallToolRequest{}, deferredReceipt)
		r.NoError(err)
		r.NotNil(deferredResult.Order)
		a.Equal(inventree.PurchaseOrderStatusPlaced, deferredResult.Order.Status)
		deferredRead, err := fixture.client.GetPurchaseOrder(ctx, deferredOrder.PK)
		r.NoError(err)
		a.Equal(deferredResult.Order.Status, deferredRead.Status)

		_, completionPlan, err := completePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, CompletePurchaseOrderInput{DryRun: true, OrderID: deferredOrder.PK})
		r.NoError(err)
		r.NotEmpty(completionPlan.PlanHash)
		r.Len(completionPlan.Lines, 2)
		_, completedLater, err := completePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, CompletePurchaseOrderInput{OrderID: deferredOrder.PK, ConfirmComplete: true, PlanHash: completionPlan.PlanHash})
		r.NoError(err)
		r.NotNil(completedLater.Order)
		a.Equal(inventree.PurchaseOrderStatusComplete, completedLater.Order.Status)
		completedLaterRead, err := fixture.client.GetPurchaseOrder(ctx, deferredOrder.PK)
		r.NoError(err)
		a.Equal(completedLater.Order.Status, completedLaterRead.Status)

		inlineOrder, inlineLines := createFullyReceivableOrder("po-inline-completion")
		inlineReceipt := ReceivePurchaseOrderInput{DryRun: true, CompleteOrder: true, OrderID: inlineOrder.PK, Items: []ReceivePurchaseOrderItem{{LineItemID: inlineLines[0].PK, Quantity: 2}, {LineItemID: inlineLines[1].PK, Quantity: 1}}}
		_, inlinePlan, err := receivePurchaseOrderItems(fixture.deps())(ctx, &mcp.CallToolRequest{}, inlineReceipt)
		r.NoError(err)
		r.Len(inlinePlan.CompletionLines, 2)
		inlineReceipt.DryRun = false
		inlineReceipt.ConfirmReceive = true
		inlineReceipt.PlanHash = inlinePlan.PlanHash
		_, completedInline, err := receivePurchaseOrderItems(fixture.deps())(ctx, &mcp.CallToolRequest{}, inlineReceipt)
		r.NoError(err)
		r.NotNil(completedInline.Order)
		a.Equal(inventree.PurchaseOrderStatusComplete, completedInline.Order.Status)
		a.Equal(CompletePurchaseOrderToolName, completedInline.CompletionAction)
		completedInlineRead, err := fixture.client.GetPurchaseOrder(ctx, inlineOrder.PK)
		r.NoError(err)
		a.Equal(completedInline.Order.Status, completedInlineRead.Status)
	})

	t.Run("purchase_order_hold_resume_and_cancel", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		destination := fixture.ensure(t, testenv.FixtureLocation)

		newOrder := func(suffix string) inventree.PurchaseOrder {
			t.Helper()
			reference, err := fixture.run.Name(suffix)
			r.NoError(err)
			order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID, SupplierReference: &reference})
			r.NoError(err)
			return order
		}

		// Pinned live InvenTree 1.5.1/API 530 behavior: hold/issue succeed
		// unconditionally from PENDING or PLACED with no native source-state
		// validation, and resume reuses the issue endpoint (no dedicated
		// resume endpoint exists), always landing on PLACED regardless of
		// whether the order was held from PENDING or PLACED. This subtest
		// pins the MCP-layer guards added on top of that permissiveness.

		t.Run("hold_and_resume_round_trip_from_placed", func(t *testing.T) {
			order := newOrder("po-hold-resume-placed")
			lineReference, err := fixture.run.Name("po-hold-resume-placed-line")
			r.NoError(err)
			_, err = fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &lineReference, Quantity: 1, Destination: &destination.ID})
			r.NoError(err)
			r.NoError(fixture.client.IssuePurchaseOrder(ctx, order.PK))

			_, holdPlan, err := holdPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{DryRun: true, OrderID: order.PK})
			r.NoError(err)
			r.NotEmpty(holdPlan.PlanHash)
			a.Empty(holdPlan.Warning, "an order held from PLACED must not warn about silently placing an unissued order")
			_, held, err := holdPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{OrderID: order.PK, Confirm: true, PlanHash: holdPlan.PlanHash})
			r.NoError(err)
			r.NotNil(held.Order)
			a.Equal(inventree.PurchaseOrderStatusOnHold, held.Order.Status)
			heldRead, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
			r.NoError(err)
			a.Equal(held.Order.Status, heldRead.Status)

			_, resumePlan, err := resumePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, ResumePurchaseOrderInput{DryRun: true, OrderID: order.PK})
			r.NoError(err)
			r.NotEmpty(resumePlan.PlanHash)
			_, resumed, err := resumePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, ResumePurchaseOrderInput{OrderID: order.PK, Confirm: true, PlanHash: resumePlan.PlanHash})
			r.NoError(err)
			r.NotNil(resumed.Order)
			a.Equal(inventree.PurchaseOrderStatusPlaced, resumed.Order.Status)
			resumedRead, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
			r.NoError(err)
			a.Equal(resumed.Order.Status, resumedRead.Status)
		})

		t.Run("hold_from_pending_warns_and_resume_places_the_order", func(t *testing.T) {
			order := newOrder("po-hold-resume-pending")
			_, holdPlan, err := holdPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{DryRun: true, OrderID: order.PK})
			r.NoError(err)
			a.NotEmpty(holdPlan.Warning, "holding a never-placed order must warn that resuming will place it with its supplier")
			_, held, err := holdPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{OrderID: order.PK, Confirm: true, PlanHash: holdPlan.PlanHash})
			r.NoError(err)
			r.NotNil(held.Order)
			a.Equal(inventree.PurchaseOrderStatusOnHold, held.Order.Status)

			_, resumePlan, err := resumePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, ResumePurchaseOrderInput{DryRun: true, OrderID: order.PK})
			r.NoError(err)
			_, resumed, err := resumePurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, ResumePurchaseOrderInput{OrderID: order.PK, Confirm: true, PlanHash: resumePlan.PlanHash})
			r.NoError(err)
			r.NotNil(resumed.Order)
			a.Equal(inventree.PurchaseOrderStatusPlaced, resumed.Order.Status, "InvenTree's issue endpoint always lands on PLACED, even for an order that was never placed before its hold")
		})

		t.Run("cancel_an_unreceived_order", func(t *testing.T) {
			order := newOrder("po-cancel-unreceived")
			_, cancelPlan, err := cancelPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, CancelPurchaseOrderInput{DryRun: true, OrderID: order.PK})
			r.NoError(err)
			r.NotEmpty(cancelPlan.PlanHash)
			_, cancelled, err := cancelPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, CancelPurchaseOrderInput{OrderID: order.PK, Confirm: true, PlanHash: cancelPlan.PlanHash})
			r.NoError(err)
			r.NotNil(cancelled.Order)
			a.Equal(inventree.PurchaseOrderStatusCancelled, cancelled.Order.Status)
			cancelledRead, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
			r.NoError(err)
			a.Equal(cancelled.Order.Status, cancelledRead.Status)
		})

		t.Run("cancel_refuses_a_partially_received_order", func(t *testing.T) {
			order := newOrder("po-cancel-received")
			lineReference, err := fixture.run.Name("po-cancel-received-line")
			r.NoError(err)
			line, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &lineReference, Quantity: 2, Destination: &destination.ID})
			r.NoError(err)
			r.NoError(fixture.client.IssuePurchaseOrder(ctx, order.PK))
			_, err = fixture.client.ReceivePurchaseOrder(ctx, order.PK, inventree.PurchaseOrderReceive{Items: []inventree.PurchaseOrderReceiveItem{{LineItem: line.PK, Location: &destination.ID, Quantity: "1"}}})
			r.NoError(err)

			_, refused, err := cancelPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, CancelPurchaseOrderInput{DryRun: true, OrderID: order.PK})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, refused.Status)
			r.NotNil(refused.Clarification)
			a.Equal("line_item_id", refused.Clarification.Retry)
			unchanged, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
			r.NoError(err)
			a.Equal(inventree.PurchaseOrderStatusPlaced, unchanged.Status, "the MCP-layer received-quantity guard must refuse before InvenTree's own permissive cancel endpoint is called")
		})

		t.Run("cancel_refuses_a_completed_order", func(t *testing.T) {
			order := newOrder("po-cancel-complete")
			lineReference, err := fixture.run.Name("po-cancel-complete-line")
			r.NoError(err)
			line, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &lineReference, Quantity: 1, Destination: &destination.ID})
			r.NoError(err)
			r.NoError(fixture.client.IssuePurchaseOrder(ctx, order.PK))
			_, err = fixture.client.ReceivePurchaseOrder(ctx, order.PK, inventree.PurchaseOrderReceive{Items: []inventree.PurchaseOrderReceiveItem{{LineItem: line.PK, Location: &destination.ID, Quantity: "1"}}})
			r.NoError(err)
			r.NoError(fixture.client.CompletePurchaseOrder(ctx, order.PK))

			_, refused, err := cancelPurchaseOrder(fixture.deps())(ctx, &mcp.CallToolRequest{}, CancelPurchaseOrderInput{DryRun: true, OrderID: order.PK})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, refused.Status)
		})
	})

	t.Run("purchase_order_line_delete_happy_path", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		supplierPart := fixture.ensure(t, testenv.FixtureSupplierPart)
		destination := fixture.ensure(t, testenv.FixtureLocation)

		supplierPartDetail, err := fixture.client.GetSupplierPart(ctx, supplierPart.ID)
		r.NoError(err)

		order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID, OrderCurrency: dvgoutils.Ptr("AUD")})
		r.NoError(err)

		mistakenReference, err := fixture.run.Name("po-line-delete-mistaken")
		r.NoError(err)
		mistakenLine, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &mistakenReference, Quantity: 2, PurchasePrice: dvgoutils.Ptr("1.5"), PurchasePriceCurrency: dvgoutils.Ptr("AUD"), Destination: &destination.ID})
		r.NoError(err)

		orderBeforeDelete, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		r.NotNil(orderBeforeDelete.TotalPrice)

		_, preview, err := deletePurchaseOrderLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: mistakenLine.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, preview.Status)
		r.NotNil(preview.Record)
		a.Equal(mistakenLine.PK, preview.Record.PK)
		r.NotNil(preview.PurchaseOrder)
		a.Equal(order.PK, preview.PurchaseOrder.PK)
		a.Equal(supplierPartDetail.Part, preview.PartID)

		_, deleted, err := deletePurchaseOrderLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: mistakenLine.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusOK, deleted.Status)
		a.True(deleted.Verified)
		r.NotNil(deleted.PurchaseOrder)
		r.NotNil(deleted.PurchaseOrder.TotalPrice)
		a.NotEqual(*orderBeforeDelete.TotalPrice, *deleted.PurchaseOrder.TotalPrice, "removing the priced mistaken line must change the refreshed order total")
		_, err = fixture.client.GetPurchaseOrderLine(ctx, mistakenLine.PK)
		r.Error(err, "the deleted line must no longer exist")

		correctedReference, err := fixture.run.Name("po-extra-line-corrected")
		r.NoError(err)
		_, correctedExtraLine, err := createPurchaseOrderExtraLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreatePurchaseOrderExtraLineInput{OrderID: order.PK, Reference: correctedReference, Description: dvgoutils.Ptr("corrected non-stock line"), Quantity: 1, UnitPrice: dvgoutils.Ptr("3"), Currency: dvgoutils.Ptr("AUD")})
		r.NoError(err)
		a.Equal(StatusOK, correctedExtraLine.Status)

		receivableReference, err := fixture.run.Name("po-line-delete-receivable")
		r.NoError(err)
		receivableLine, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &receivableReference, Quantity: 3, PurchasePrice: dvgoutils.Ptr("1"), PurchasePriceCurrency: dvgoutils.Ptr("AUD"), Destination: &destination.ID})
		r.NoError(err)

		r.NoError(fixture.client.IssuePurchaseOrder(ctx, order.PK))
		placedOrder, err := fixture.client.GetPurchaseOrder(ctx, order.PK)
		r.NoError(err)
		a.Equal(inventree.PurchaseOrderStatusPlaced, placedOrder.Status)

		placedMistakeReference, err := fixture.run.Name("po-line-delete-placed-mistake")
		r.NoError(err)
		placedMistakeLine, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &placedMistakeReference, Quantity: 1})
		r.NoError(err, "adding a line to a PLACED order must still be possible")
		_, deletedFromPlaced, err := deletePurchaseOrderLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: placedMistakeLine.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusOK, deletedFromPlaced.Status, "an unreceived line on a PLACED order must still be deletable")

		received, err := fixture.client.ReceivePurchaseOrder(ctx, order.PK, inventree.PurchaseOrderReceive{Items: []inventree.PurchaseOrderReceiveItem{{LineItem: receivableLine.PK, Location: &destination.ID, Quantity: "1"}}})
		r.NoError(err)
		r.NotEmpty(received)

		_, refusedDelete, err := deletePurchaseOrderLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: receivableLine.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, refusedDelete.Status, "the MCP tool must refuse deletion of a line with received quantity even though InvenTree itself would allow it")
		r.NotNil(refusedDelete.Clarification)
		a.Equal("received", refusedDelete.Clarification.Field)
		stillReceivable, err := fixture.client.GetPurchaseOrderLine(ctx, receivableLine.PK)
		r.NoError(err, "the refused line must still exist")
		a.Equal(1.0, stillReceivable.Received)

		linkedStockReference, err := fixture.run.Name("po-line-delete-linked-stock")
		r.NoError(err)
		linkedStockLine, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPart.ID, Reference: &linkedStockReference, Quantity: 1})
		r.NoError(err, "a second unreceived line sharing the receipted line's supplier part must still be addable")

		_, refusedLinkedStockDelete, err := deletePurchaseOrderLine(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePurchaseOrderLineInput{ID: linkedStockLine.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, refusedLinkedStockDelete.Status, "the MCP tool must refuse deletion of an unreceived line once stock already references this order and supplier part, even with zero received quantity on this specific line")
		r.NotNil(refusedLinkedStockDelete.Clarification)
		a.Equal("stock", refusedLinkedStockDelete.Clarification.Field)
		stillLinkedStock, err := fixture.client.GetPurchaseOrderLine(ctx, linkedStockLine.PK)
		r.NoError(err, "the refused line must still exist")
		a.Zero(stillLinkedStock.Received)
	})

	t.Run("stock_adjustment_happy_path", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		location := fixture.ensure(t, testenv.FixtureLocation)
		batch, err := fixture.run.Name("stocktake")
		r.NoError(err)
		packaging := "reel"
		stock, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 10, Status: dvgoutils.Ptr(stockStatusAttention), Batch: &batch, Packaging: &packaging})
		r.NoError(err)

		addInput := AdjustStockQuantityInput{DryRun: true, StockItemID: stock.PK, Delta: 2, Reason: "integration count found two units"}
		_, addPlan, err := adjustStockQuantity(fixture.deps())(ctx, &mcp.CallToolRequest{}, addInput)
		r.NoError(err)
		r.NotEmpty(addPlan.PlanHash)
		addInput.DryRun = false
		addInput.Confirm = true
		addInput.PlanHash = addPlan.PlanHash
		_, added, err := adjustStockQuantity(fixture.deps())(ctx, &mcp.CallToolRequest{}, addInput)
		r.NoError(err)
		r.NotNil(added.Record)
		a.Equal(12.0, added.Record.Quantity)

		removeInput := AdjustStockQuantityInput{DryRun: true, StockItemID: stock.PK, Delta: -1, Reason: "integration damaged unit removal"}
		_, removePlan, err := adjustStockQuantity(fixture.deps())(ctx, &mcp.CallToolRequest{}, removeInput)
		r.NoError(err)
		a.True(removePlan.Plan.HighRisk)
		removeInput.DryRun = false
		removeInput.Confirm = true
		removeInput.PlanHash = removePlan.PlanHash
		_, removed, err := adjustStockQuantity(fixture.deps())(ctx, &mcp.CallToolRequest{}, removeInput)
		r.NoError(err)
		r.NotNil(removed.Record)
		a.Equal(11.0, removed.Record.Quantity)

		countInput := StocktakeAdjustmentInput{DryRun: true, StockItemID: stock.PK, ObservedQuantity: 7, Reason: "integration physical shelf count"}
		_, countPlan, err := stocktakeAdjustment(fixture.deps())(ctx, &mcp.CallToolRequest{}, countInput)
		r.NoError(err)
		a.True(countPlan.Plan.HighRisk)
		countInput.DryRun = false
		countInput.Confirm = true
		countInput.PlanHash = countPlan.PlanHash
		_, counted, err := stocktakeAdjustment(fixture.deps())(ctx, &mcp.CallToolRequest{}, countInput)
		r.NoError(err)
		r.NotNil(counted.Record)
		a.Equal(7.0, counted.Record.Quantity)
		r.NotNil(counted.Record.Location)
		a.Equal(location.ID, *counted.Record.Location)
		r.NotNil(counted.Record.Batch)
		a.Equal(batch, *counted.Record.Batch)
		a.Equal(stockStatusAttention, counted.Record.Status)
		r.NotNil(counted.Record.Packaging)
		a.Equal(packaging, *counted.Record.Packaging)

		transferDestinationName, err := fixture.run.Name("stock-transfer-destination")
		r.NoError(err)
		transferDestination, err := fixture.client.CreateStockLocation(ctx, inventree.StockLocationCreate{Name: transferDestinationName, Parent: &location.ID, External: dvgoutils.Ptr(true)})
		r.NoError(err)
		transferInput := TransferStockItemInput{DryRun: true, StockItemID: stock.PK, DestinationLocationID: transferDestination.PK, Reason: "integration move to corrected drawer"}
		_, transferPlan, err := transferStockItem(fixture.deps())(ctx, &mcp.CallToolRequest{}, transferInput)
		r.NoError(err)
		r.Equal(StatusOK, transferPlan.Status, "unexpected transfer clarification: %+v", transferPlan.Clarification)
		r.NotEmpty(transferPlan.PlanHash)
		r.NotNil(transferPlan.Plan.Transfer)
		a.False(transferPlan.Plan.Transfer.WillSplit)
		a.Equal(location.ID, transferPlan.Plan.Transfer.Source.ID)
		a.Equal(transferDestination.PK, transferPlan.Plan.Transfer.Destination.ID)
		a.True(transferPlan.Plan.Transfer.Destination.External)
		a.Equal(7.0, transferPlan.Plan.Before.Quantity)
		a.Equal(7.0, transferPlan.Plan.After.Quantity)
		transferInput.DryRun = false
		transferInput.Confirm = true
		transferInput.PlanHash = transferPlan.PlanHash
		_, transferred, err := transferStockItem(fixture.deps())(ctx, &mcp.CallToolRequest{}, transferInput)
		r.NoError(err)
		a.Equal(StatusOK, transferred.Status)
		a.True(transferred.Verified)
		a.False(transferred.Recovered)
		r.NotNil(transferred.Record)
		a.Equal(stock.PK, transferred.Record.PK)
		a.Equal(7.0, transferred.Record.Quantity)
		r.NotNil(transferred.Record.Location)
		a.Equal(transferDestination.PK, *transferred.Record.Location)
		r.NotNil(transferred.Record.Batch)
		a.Equal(batch, *transferred.Record.Batch)
		r.NotNil(transferred.Record.Packaging)
		a.Equal(packaging, *transferred.Record.Packaging)

		statusInput := SetStockStatusInput{DryRun: true, StockItemID: stock.PK, Status: stockStatusDamaged, Reason: "integration inspection found damage"}
		_, statusPlan, err := setStockStatus(fixture.deps())(ctx, &mcp.CallToolRequest{}, statusInput)
		r.NoError(err)
		statusInput.DryRun = false
		statusInput.Confirm = true
		statusInput.PlanHash = statusPlan.PlanHash
		_, changed, err := setStockStatus(fixture.deps())(ctx, &mcp.CallToolRequest{}, statusInput)
		r.NoError(err)
		r.NotNil(changed.Record)
		a.Equal(stockStatusDamaged, changed.Record.Status)
		a.Equal(7.0, changed.Record.Quantity)

		category := fixture.ensure(t, testenv.FixtureCategory)
		serializedPartName, err := fixture.run.Name("stocktake-serialized-part")
		r.NoError(err)
		serializedPart, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: serializedPartName, Category: dvgoutils.Ptr(category.ID), Active: dvgoutils.Ptr(true), Component: dvgoutils.Ptr(true), Trackable: dvgoutils.Ptr(true)})
		r.NoError(err)
		serial := "1"
		var serializedItems []inventree.StockItem
		err = fixture.client.Post(ctx, "/api/stock/", map[string]any{"part": serializedPart.PK, "location": location.ID, "quantity": 1, "serial_numbers": serial}, &serializedItems)
		r.NoError(err)
		r.Len(serializedItems, 1)
		serialized := serializedItems[0]
		serializedInput := SetStockStatusInput{DryRun: true, StockItemID: serialized.PK, Status: stockStatusAttention, Reason: "integration serialized stock inspection"}
		_, serializedPlan, err := setStockStatus(fixture.deps())(ctx, &mcp.CallToolRequest{}, serializedInput)
		r.NoError(err)
		serializedInput.DryRun = false
		serializedInput.Confirm = true
		serializedInput.PlanHash = serializedPlan.PlanHash
		_, serializedChanged, err := setStockStatus(fixture.deps())(ctx, &mcp.CallToolRequest{}, serializedInput)
		r.NoError(err)
		r.NotNil(serializedChanged.Record)
		r.NotNil(serializedChanged.Record.Serial)
		a.Equal(serial, *serializedChanged.Record.Serial)
		a.Equal(1.0, serializedChanged.Record.Quantity)

		_, serializedQuantity, err := adjustStockQuantity(fixture.deps())(ctx, &mcp.CallToolRequest{}, AdjustStockQuantityInput{DryRun: true, StockItemID: serialized.PK, Delta: -1, Reason: "integration serialized count"})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, serializedQuantity.Status)
		_, serializedCount, err := stocktakeAdjustment(fixture.deps())(ctx, &mcp.CallToolRequest{}, StocktakeAdjustmentInput{DryRun: true, StockItemID: serialized.PK, ObservedQuantity: 0, Reason: "integration serialized count"})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, serializedCount.Status)
		_, serializedDepletion, err := depleteStockItem(fixture.deps())(ctx, &mcp.CallToolRequest{}, DepleteStockItemInput{DryRun: true, StockItemID: serialized.PK, Reason: "integration unsafe serialized depletion"})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, serializedDepletion.Status)
		r.NotNil(serializedDepletion.Clarification)
		a.Equal("serial", serializedDepletion.Clarification.Field)
		_, serializedTransfer, err := transferStockItem(fixture.deps())(ctx, &mcp.CallToolRequest{}, TransferStockItemInput{DryRun: true, StockItemID: serialized.PK, DestinationLocationID: transferDestination.PK, Reason: "integration unsafe serialized transfer"})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, serializedTransfer.Status)
		r.NotNil(serializedTransfer.Clarification)
		a.Equal("serial", serializedTransfer.Clarification.Field)
		serializedStillPresent, err := fixture.client.GetStockItem(ctx, serialized.PK)
		r.NoError(err)
		a.Equal(serialized.PK, serializedStillPresent.PK)
		a.Equal(1.0, serializedStillPresent.Quantity)

		deleteOnDeplete := true
		depletionStock, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 2, DeleteOnDeplete: &deleteOnDeplete})
		r.NoError(err)
		depletionInput := DepleteStockItemInput{DryRun: true, StockItemID: depletionStock.PK, Reason: "integration remove placeholder stock"}
		_, depletionPlan, err := depleteStockItem(fixture.deps())(ctx, &mcp.CallToolRequest{}, depletionInput)
		r.NoError(err)
		r.NotEmpty(depletionPlan.PlanHash)
		a.True(depletionPlan.Plan.WillDelete)
		a.Equal(2.0, depletionPlan.Plan.Before.Quantity)
		r.NotNil(depletionPlan.Plan.Depletion)
		r.NotNil(depletionPlan.Plan.Depletion.Allocated)
		a.Zero(*depletionPlan.Plan.Depletion.Allocated)
		r.NotNil(depletionPlan.Plan.Depletion.InstalledItems)
		a.Zero(*depletionPlan.Plan.Depletion.InstalledItems)
		r.NotNil(depletionPlan.Plan.Depletion.ChildItems)
		a.Zero(*depletionPlan.Plan.Depletion.ChildItems)
		depletionInput.DryRun = false
		depletionInput.Confirm = true
		depletionInput.PlanHash = depletionPlan.PlanHash
		_, depleted, err := depleteStockItem(fixture.deps())(ctx, &mcp.CallToolRequest{}, depletionInput)
		r.NoError(err)
		a.Equal(StatusOK, depleted.Status)
		a.True(depleted.Verified)
		a.False(depleted.Recovered)
		_, err = fixture.client.GetStockItem(ctx, depletionStock.PK)
		r.Error(err)

		lostStock, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 1, DeleteOnDeplete: &deleteOnDeplete})
		r.NoError(err)
		lostClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPost, path: "/api/stock/remove/"}})
		r.NoError(err)
		lostDeps := fixture.deps()
		lostDeps.ClientFromContext = func(context.Context) (any, error) { return lostClient, nil }
		lostInput := DepleteStockItemInput{DryRun: true, StockItemID: lostStock.PK, Reason: "integration response-loss depletion"}
		_, lostPlan, err := depleteStockItem(lostDeps)(ctx, &mcp.CallToolRequest{}, lostInput)
		r.NoError(err)
		lostInput.DryRun = false
		lostInput.Confirm = true
		lostInput.PlanHash = lostPlan.PlanHash
		_, recovered, err := depleteStockItem(lostDeps)(ctx, &mcp.CallToolRequest{}, lostInput)
		r.NoError(err)
		a.Equal(StatusOK, recovered.Status)
		a.True(recovered.Verified)
		a.True(recovered.Recovered)

		lostTransferStock, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 3, Batch: dvgoutils.Ptr("response-loss-transfer")})
		r.NoError(err)
		lostTransferClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPost, path: "/api/stock/transfer/"}})
		r.NoError(err)
		lostTransferDeps := fixture.deps()
		lostTransferDeps.ClientFromContext = func(context.Context) (any, error) { return lostTransferClient, nil }
		lostTransferInput := TransferStockItemInput{DryRun: true, StockItemID: lostTransferStock.PK, DestinationLocationID: transferDestination.PK, Reason: "integration response-loss transfer"}
		_, lostTransferPlan, err := transferStockItem(lostTransferDeps)(ctx, &mcp.CallToolRequest{}, lostTransferInput)
		r.NoError(err)
		lostTransferInput.DryRun = false
		lostTransferInput.Confirm = true
		lostTransferInput.PlanHash = lostTransferPlan.PlanHash
		_, recoveredTransfer, err := transferStockItem(lostTransferDeps)(ctx, &mcp.CallToolRequest{}, lostTransferInput)
		r.NoError(err)
		a.Equal(StatusOK, recoveredTransfer.Status)
		a.True(recoveredTransfer.Verified)
		a.True(recoveredTransfer.Recovered)
		r.NotNil(recoveredTransfer.Record)
		a.Equal(lostTransferStock.PK, recoveredTransfer.Record.PK)
		r.NotNil(recoveredTransfer.Record.Location)
		a.Equal(transferDestination.PK, *recoveredTransfer.Record.Location)
	})

	t.Run("attachment_target_matrix_upload_download_and_max_bytes", func(t *testing.T) {
		for _, modelType := range attachmentTargetModelTypes() {
			t.Run(modelType, func(t *testing.T) {
				r := require.New(t)
				a := assert.New(t)
				ctx, _, _ := testhandler.SetupTestHandler(t)
				fixture := newMilestoneToolFixture(t, shared)
				target := fixture.attachmentTarget(t, modelType)
				content := []byte("milestone attachment bytes for " + modelType)
				filename := modelType + "-readback.txt"

				_, uploaded, err := uploadAttachment(fixture.deps())(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
					ModelType:    modelType,
					ModelID:      target.modelID,
					Filename:     filename,
					ContentType:  "text/plain",
					InlineBase64: base64.StdEncoding.EncodeToString(content),
				})
				r.NoError(err)
				a.Equal(StatusOK, uploaded.Status)
				a.Equal("inline", uploaded.SourceKind)
				r.NotZero(uploaded.Record.PK)

				_, downloaded, err := downloadAttachment(fixture.deps())(ctx, &mcp.CallToolRequest{}, DownloadInput{
					ID:       uploaded.Record.PK,
					Mode:     string(inventree.AttachmentContentOriginal),
					MaxBytes: int64(len(content) + 1),
				})
				r.NoError(err)
				a.Equal(StatusOK, downloaded.Status)
				a.Equal(filename, downloaded.Filename)
				a.Equal(string(content), downloaded.Text)
				a.Equal(len(content), downloaded.Size)
				a.Equal(sha256Hex(content), downloaded.SHA256)
				a.Equal(string(inventree.AttachmentContentOriginal), downloaded.Mode)

				_, _, err = downloadAttachment(fixture.deps())(ctx, &mcp.CallToolRequest{}, DownloadInput{
					ID:       uploaded.Record.PK,
					Mode:     string(inventree.AttachmentContentOriginal),
					MaxBytes: int64(len(content) - 1),
				})
				r.ErrorContains(err, "exceeds maxBytes")
			})
		}
	})

	t.Run("live_order_entry_without_mpn_is_recoverable", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		manufacturer := fixture.ensure(t, testenv.FixtureManufacturer)
		partName, err := fixture.run.Name("order-entry-part")
		r.NoError(err)
		sku, err := fixture.run.Name("order-entry-sku")
		r.NoError(err)

		_, partPlan, err := upsertPartWorkflow(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
			DryRun:         true,
			Name:           partName,
			CategoryID:     category.ID,
			SupplierID:     supplier.ID,
			SupplierSKU:    sku,
			ManufacturerID: manufacturer.ID,
			MPN:            dvgoutils.Ptr("  \t "),
		})
		r.NoError(err)
		a.Equal(StatusOK, partPlan.Status)
		a.True(partPlan.DryRun)

		_, created, err := upsertPartWorkflow(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
			Name:           partName,
			CategoryID:     category.ID,
			SupplierID:     supplier.ID,
			SupplierSKU:    sku,
			ManufacturerID: manufacturer.ID,
			MPN:            dvgoutils.Ptr("  \t "),
		})
		r.NoError(err)
		a.Equal(StatusOK, created.Status, "workflow output: %#v", created)
		r.NotNil(created.Part)
		a.Nil(created.ManufacturerPart)
		r.NotNil(created.SupplierPart)

		_, parts, err := searchParts(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchInput{Search: partName, Limit: MaxLookupLimit})
		r.NoError(err)
		a.Equal(StatusOK, parts.Status)
		partIDs := make([]int, 0, len(parts.Results))
		for _, part := range parts.Results {
			partIDs = append(partIDs, part.PK)
		}
		a.Contains(partIDs, created.Part.PK)

		_, manufacturerParts, err := searchManufacturerPartsAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, ManufacturerPartSearchInput{PartID: created.Part.PK, ManufacturerID: manufacturer.ID})
		r.NoError(err)
		a.Equal(StatusOK, manufacturerParts.Status)
		a.Empty(manufacturerParts.Results)

		_, rejectedManufacturerPart, err := createManufacturerPart(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateManufacturerPartInput{
			PartID:         created.Part.PK,
			ManufacturerID: manufacturer.ID,
		})
		r.NoError(err)
		a.Equal(StatusValidationFailed, rejectedManufacturerPart.Status)
		r.NotNil(rejectedManufacturerPart.Validation)
		a.Equal([]ValidationFieldError{{Field: "MPN", Messages: []string{"This field may not be blank."}}}, rejectedManufacturerPart.Validation.Fields)

		_, supplierParts, err := searchSupplierPartsAdmin(fixture.deps())(ctx, &mcp.CallToolRequest{}, SupplierPartSearchInput{PartID: created.Part.PK, SupplierID: supplier.ID, SKU: sku})
		r.NoError(err)
		a.Equal(StatusOK, supplierParts.Status)
		r.Len(supplierParts.Results, 1)
		a.Equal(created.SupplierPart.PK, supplierParts.Results[0].ID)

		supplierReference, err := fixture.run.Name("ebay-order")
		r.NoError(err)
		unitPrice := 1.25
		orderInput := PurchaseOrderWorkflowInput{
			DryRun:            true,
			SupplierID:        supplier.ID,
			SupplierReference: supplierReference,
			Description:       dvgoutils.Ptr("sanitized live order-entry regression"),
			Lines: []PurchaseOrderWorkflowLine{{
				SupplierPartID: created.SupplierPart.PK,
				Quantity:       1,
				UnitPrice:      &unitPrice,
				Currency:       "AUD",
			}},
		}
		_, orderPlan, err := createPurchaseOrderWithLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, orderInput)
		r.NoError(err)
		a.Equal(StatusOK, orderPlan.Status)
		a.True(orderPlan.DryRun)

		orderInput.DryRun = false
		_, order, err := createPurchaseOrderWithLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, orderInput)
		r.NoError(err)
		a.Equal(StatusOK, order.Status)
		r.NotNil(order.PurchaseOrder)
		r.Len(order.Lines, 1)

		_, orders, err := searchPurchaseOrders(fixture.deps())(ctx, &mcp.CallToolRequest{}, PurchaseOrderSearchInput{Search: supplierReference, SupplierID: supplier.ID, Limit: MaxLookupLimit})
		r.NoError(err)
		a.Equal(StatusOK, orders.Status)
		orderIDs := make([]int, 0, len(orders.Results))
		for _, purchaseOrder := range orders.Results {
			orderIDs = append(orderIDs, purchaseOrder.PK)
		}
		a.Contains(orderIDs, order.PurchaseOrder.PK)

		_, lines, err := searchPurchaseOrderLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, PurchaseOrderLineSearchInput{OrderID: order.PurchaseOrder.PK, SupplierPartID: created.SupplierPart.PK, Limit: MaxLookupLimit})
		r.NoError(err)
		a.Equal(StatusOK, lines.Status)
		r.Len(lines.Results, 1)
		a.Equal(order.Lines[0].PK, lines.Results[0].PK)
	})

	t.Run("delete_attachment_missing_confirm_returns_structured_clarification_through_mcp", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		fixture := newMilestoneToolFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)

		_, uploaded, err := uploadAttachment(fixture.deps())(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
			ModelType:    "part",
			ModelID:      part.ID,
			Filename:     "delete-confirm-readback.txt",
			ContentType:  "text/plain",
			InlineBase64: base64.StdEncoding.EncodeToString([]byte("delete confirmation boundary")),
		})
		r.NoError(err)
		r.NotZero(uploaded.Record.PK)

		clientTransport, serverTransport := mcp.NewInMemoryTransports()
		serverDone := make(chan error, 1)
		go func() {
			mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "v0.0.0"}, nil)
			deps := fixture.deps()
			deps.EnableWriteTools = true
			Register(mcpServer, deps)
			serverDone <- mcpServer.Run(ctx, serverTransport)
		}()

		client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
		session, err := client.Connect(ctx, clientTransport, nil)
		r.NoError(err)
		defer func() {
			r.NoError(session.Close())
			cancel()
			<-serverDone
		}()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      DeleteAttachmentToolName,
			Arguments: map[string]any{"id": uploaded.Record.PK},
		})
		r.NoError(err)
		a.False(result.IsError)
		structured := result.StructuredContent.(map[string]any)
		a.Equal(StatusClarificationRequired, structured["status"])
		clarification := structured["clarification"].(map[string]any)
		a.Equal(StatusClarificationRequired, clarification["status"])
		a.Equal("confirm", clarification["retry"])

		metadata, err := fixture.client.GetAttachmentMetadata(ctx, uploaded.Record.PK)
		r.NoError(err)
		a.Equal(uploaded.Record.PK, metadata.PK, "missing confirm must not delete the attachment")
	})

	t.Run("local_path_url_link_and_primary_image_happy_paths", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		allowRoot := t.TempDir()
		localContent := []byte("local path attachment bytes")
		localPath := filepath.Join(allowRoot, "local-readback.txt")
		r.NoError(os.WriteFile(localPath, localContent, 0o644))
		deps := fixture.deps()
		deps.UploadMode = upload.ModeStdio
		deps.UploadFS = afero.NewOsFs()
		deps.UploadAllowRoots = []string{allowRoot}

		_, localUpload, err := uploadAttachment(deps)(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
			ModelType:   "part",
			ModelID:     part.ID,
			ContentType: "text/plain",
			LocalPath:   localPath,
		})
		r.NoError(err)
		a.Equal(StatusOK, localUpload.Status)
		a.Equal("local_path", localUpload.SourceKind)
		_, localDownload, err := downloadAttachment(fixture.deps())(ctx, &mcp.CallToolRequest{}, DownloadInput{ID: localUpload.Record.PK, MaxBytes: 1024})
		r.NoError(err)
		a.Equal(string(localContent), localDownload.Text)
		a.Equal(sha256Hex(localContent), localDownload.SHA256)

		outsidePath := filepath.Join(t.TempDir(), "outside.txt")
		r.NoError(os.WriteFile(outsidePath, []byte("outside"), 0o644))
		_, outsideUpload, err := uploadAttachment(deps)(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
			ModelType:   "part",
			ModelID:     part.ID,
			ContentType: "text/plain",
			LocalPath:   outsidePath,
		})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, outsideUpload.Status)
		r.NotNil(outsideUpload.LocalUploadRecovery)
		a.Equal(LocalUploadReasonOutsideAllowlist, outsideUpload.LocalUploadRecovery.Reason)
		a.Equal(GetLocalUploadPolicyToolName, outsideUpload.LocalUploadRecovery.PolicyTool)

		var fetchedAuthHeaders []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			fetchedAuthHeaders = append(fetchedAuthHeaders, req.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Disposition", `attachment; filename="url-readback.txt"`)
			_, _ = w.Write([]byte("url upload bytes"))
		}))
		t.Cleanup(server.Close)
		deps = fixture.deps()
		deps.URLFetcher = allowLocalTestServerFetcher(t, server.URL)
		_, urlUpload, err := uploadAttachmentFromURL(deps)(ctx, &mcp.CallToolRequest{}, UploadAttachmentFromURLInput{
			ModelType: "part",
			ModelID:   part.ID,
			URL:       server.URL + "/url-readback.txt",
		})
		r.NoError(err)
		a.Equal(StatusOK, urlUpload.Status)
		a.Equal("url", urlUpload.SourceKind)
		a.Equal([]string{""}, fetchedAuthHeaders)
		_, urlDownload, err := downloadAttachment(fixture.deps())(ctx, &mcp.CallToolRequest{}, DownloadInput{ID: urlUpload.Record.PK, MaxBytes: 1024})
		r.NoError(err)
		a.Equal("url upload bytes", urlDownload.Text)

		fetchCount := 0
		linkServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			fetchCount++
		}))
		t.Cleanup(linkServer.Close)
		_, link, err := createLinkAttachment(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateLinkAttachmentInput{
			ModelType: "part",
			ModelID:   part.ID,
			URL:       linkServer.URL + "/stored-only",
		})
		r.NoError(err)
		a.Equal(StatusOK, link.Status)
		a.Equal("link", link.SourceKind)
		a.Equal(0, fetchCount)
		_, err = fixture.client.DownloadAttachment(ctx, link.Record.PK, inventree.AttachmentContentOriginal, 1024)
		r.ErrorContains(err, "no file attachment URL")

		imageBytes := tinyPNGBytes()
		_, imageAttachment, err := uploadAttachment(fixture.deps())(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
			ModelType:    "part",
			ModelID:      part.ID,
			Filename:     "primary.png",
			ContentType:  "image/png",
			InlineBase64: base64.StdEncoding.EncodeToString(imageBytes),
		})
		r.NoError(err)
		a.Equal(StatusOK, imageAttachment.Status)
		_, primary, err := setPrimaryImage(fixture.deps())(ctx, &mcp.CallToolRequest{}, SetPrimaryImageInput{
			PartID:       part.ID,
			AttachmentID: imageAttachment.Record.PK,
		})
		r.NoError(err)
		a.Equal(StatusOK, primary.Status)
		a.False(primary.Replaced)
		_, partImage, err := downloadPartImage(fixture.deps())(ctx, &mcp.CallToolRequest{}, PartImageDownloadInput{
			ID:       part.ID,
			Mode:     string(inventree.AttachmentContentOriginal),
			MaxBytes: int64(len(imageBytes) + 1),
		})
		r.NoError(err)
		a.Equal(StatusOK, partImage.Status)
		a.Equal(sha256Hex(imageBytes), partImage.SHA256)
		a.Equal(base64.StdEncoding.EncodeToString(imageBytes), partImage.Base64)

		_, _, err = downloadPartImage(fixture.deps())(ctx, &mcp.CallToolRequest{}, PartImageDownloadInput{
			ID:       part.ID,
			Mode:     string(inventree.AttachmentContentOriginal),
			MaxBytes: int64(len(imageBytes) - 1),
		})
		r.ErrorContains(err, "exceeds maxBytes")

		_, thumbnail, err := downloadPartImage(fixture.deps())(ctx, &mcp.CallToolRequest{}, PartImageDownloadInput{
			ID:       part.ID,
			Mode:     string(inventree.AttachmentContentThumbnail),
			MaxBytes: 4096,
		})
		r.NoError(err)
		a.Equal(StatusOK, thumbnail.Status)
		a.Equal(string(inventree.AttachmentContentThumbnail), thumbnail.Mode)
		a.NotZero(thumbnail.Size)

		noImagePart := fixture.createPart(t, "noimage")
		_, noImage, err := downloadPartImage(fixture.deps())(ctx, &mcp.CallToolRequest{}, PartImageDownloadInput{ID: noImagePart.PK})
		r.NoError(err)
		a.Equal(StatusNoImage, noImage.Status)
	})

	t.Run("company_primary_images", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		initialBytes := tinyPNGColorBytes(color.NRGBA{R: 255, A: 255})
		replacementBytes := tinyPNGColorBytes(color.NRGBA{B: 255, A: 255})
		jpegBytes := tinyJPEGBytes()
		webpBytes := tinyWebPBytes()
		type roleCase struct {
			name                   string
			supplier, manufacturer bool
			customer               bool
			content                []byte
			extension, contentType string
		}
		roles := []roleCase{
			{name: "none"},
			{name: "supplier", supplier: true},
			{name: "manufacturer", manufacturer: true, content: jpegBytes, extension: ".jpg", contentType: "image/jpeg"},
			{name: "customer", customer: true, content: webpBytes, extension: ".webp", contentType: "image/webp"},
			{name: "supplier-manufacturer", supplier: true, manufacturer: true},
			{name: "supplier-customer", supplier: true, customer: true},
			{name: "manufacturer-customer", manufacturer: true, customer: true},
			{name: "mixed", supplier: true, manufacturer: true, customer: true},
		}
		companies := make(map[string]inventree.CompanyDetail, len(roles))
		for _, role := range roles {
			name, err := fixture.run.Name("company-image-" + role.name)
			r.NoError(err)
			created, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{
				Name: name, Currency: "AUD", IsSupplier: role.supplier, IsManufacturer: role.manufacturer,
			})
			r.NoError(err)
			_, err = fixture.client.UpdateCompany(ctx, created.PK, inventree.PatchFields{
				"is_supplier": inventree.Set(role.supplier), "is_manufacturer": inventree.Set(role.manufacturer), "is_customer": inventree.Set(role.customer),
			})
			r.NoError(err)
			before, err := fixture.client.GetCompanyDetail(ctx, created.PK)
			r.NoError(err)
			r.Nil(before.Image)
			content := role.content
			extension := role.extension
			contentType := role.contentType
			if content == nil {
				content, extension, contentType = initialBytes, ".png", "image/png"
			}

			_, output, err := setCompanyImage(fixture.deps())(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
				CompanyID: created.PK, InlineBase64: base64.StdEncoding.EncodeToString(content), Filename: role.name + extension, ContentType: contentType,
			})
			r.NoError(err)
			a.Equal(StatusOK, output.Status)
			a.True(output.Verified)
			a.False(output.Replaced)
			r.NotNil(output.Image)
			a.Equal(sha256Hex(content), output.Image.SHA256)
			a.Equal(role.supplier, before.IsSupplier)
			a.Equal(role.manufacturer, before.IsManufacturer)
			a.Equal(role.customer, before.IsCustomer)
			a.Equal(role.supplier, outputRoleCompany(t, ctx, fixture.client, created.PK).IsSupplier)
			a.Equal(role.manufacturer, outputRoleCompany(t, ctx, fixture.client, created.PK).IsManufacturer)
			a.Equal(role.customer, outputRoleCompany(t, ctx, fixture.client, created.PK).IsCustomer)
			companies[role.name] = before
		}

		mixed := companies["mixed"]
		_, _, err := setCompanyImage(fixture.deps())(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
			CompanyID: mixed.PK, InlineBase64: base64.StdEncoding.EncodeToString([]byte("not an image")), Filename: "invalid.png", ContentType: "image/png", Confirm: true,
		})
		r.ErrorContains(err, "not a supported valid raster image")
		unchanged, err := fixture.client.DownloadCompanyImage(ctx, mixed.PK, 1024)
		r.NoError(err)
		a.Equal(initialBytes, unchanged.Content)

		var fetchedAuth []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			fetchedAuth = append(fetchedAuth, req.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Disposition", `attachment; filename="replacement.png"`)
			_, _ = w.Write(replacementBytes)
		}))
		t.Cleanup(server.Close)
		urlDeps := fixture.deps()
		urlDeps.URLFetcher = allowLocalTestServerFetcher(t, server.URL)
		_, replaced, err := setCompanyImageFromURL(urlDeps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageFromURLInput{
			CompanyID: mixed.PK, URL: server.URL + "/replacement.png", Confirm: true,
		})
		r.NoError(err)
		a.Equal(StatusOK, replaced.Status)
		a.True(replaced.Replaced)
		a.True(replaced.Verified)
		a.Equal(sha256Hex(replacementBytes), replaced.Image.SHA256)
		a.Equal([]string{""}, fetchedAuth)

		supplier := companies["supplier"]
		lostSetClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{
			base: http.DefaultTransport, method: http.MethodPatch, path: fmt.Sprintf("/api/company/%d/", supplier.PK),
		}})
		r.NoError(err)
		lostSetDeps := fixture.deps()
		lostSetDeps.ClientFromContext = func(context.Context) (any, error) { return lostSetClient, nil }
		_, recoveredSet, err := setCompanyImage(lostSetDeps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
			CompanyID: supplier.PK, InlineBase64: base64.StdEncoding.EncodeToString(replacementBytes), Filename: "recovered.png", ContentType: "image/png", Confirm: true,
		})
		r.NoError(err)
		a.Equal(StatusOK, recoveredSet.Status)
		a.True(recoveredSet.Recovered)
		a.True(recoveredSet.Verified)

		for name, company := range companies {
			deps := fixture.deps()
			if name == "mixed" {
				lostClearClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{
					base: http.DefaultTransport, method: http.MethodPatch, path: fmt.Sprintf("/api/company/%d/", company.PK),
				}})
				r.NoError(err)
				deps.ClientFromContext = func(context.Context) (any, error) { return lostClearClient, nil }
			}
			_, cleared, err := clearCompanyImage(deps)(ctx, &mcp.CallToolRequest{}, ClearCompanyImageInput{CompanyID: company.PK, Confirm: true})
			r.NoError(err)
			a.Equal(StatusOK, cleared.Status)
			a.True(cleared.Verified)
			a.Equal(name == "mixed", cleared.Recovered)
			a.Nil(outputRoleCompany(t, ctx, fixture.client, company.PK).Image)
		}
	})

	t.Run("global_parameter_search_and_confirmed_delete", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		partFixture := fixture.ensure(t, testenv.FixturePart)
		part, err := fixture.client.GetPart(ctx, partFixture.ID)
		r.NoError(err)
		r.NotNil(part.Category)
		templateName, err := fixture.run.Name("parameter-search")
		r.NoError(err)
		var template inventree.ParameterTemplate
		r.NoError(fixture.client.Post(ctx, "/api/parameter/template/", map[string]any{
			"name": templateName, "description": "F-S12 integration template", "units": "ohm", "enabled": true,
		}, &template))
		parameter, err := fixture.client.CreatePartParameter(ctx, inventree.NewPartParameter(part.PK, template.PK, "12k"))
		r.NoError(err)
		value := "12k"

		_, searched, err := searchPartParameterValues(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchPartParametersInput{
			TemplateName: templateName,
			Value:        &value,
			CategoryID:   *part.Category,
		})
		r.NoError(err)
		a.Equal(StatusOK, searched.Status)
		r.Len(searched.Results, 1)
		a.Equal(parameter.PK, searched.Results[0].ParameterID)
		a.Equal(part.PK, searched.Results[0].PartID)
		a.Equal(template.PK, searched.Results[0].TemplateID)
		a.Equal("12k", searched.Results[0].Value)

		_, confirmation, err := deletePartParameter(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePartParameterInput{ParameterID: parameter.PK})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, confirmation.Status)
		_, deleted, err := deletePartParameter(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeletePartParameterInput{ParameterID: parameter.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusOK, deleted.Status)
		a.True(deleted.Verified)

		_, afterDelete, err := searchPartParameterValues(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchPartParametersInput{TemplateID: template.PK, PartID: part.PK})
		r.NoError(err)
		a.Equal(StatusNotFound, afterDelete.Status)
	})

	t.Run("parameter_template_admin_and_confirmed_merge", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		part := fixture.ensure(t, testenv.FixturePart)
		sourceName, err := fixture.run.Name("template-source")
		r.NoError(err)
		targetName, err := fixture.run.Name("template-target")
		r.NoError(err)
		units, description, modelType, choices, checkbox, enabled := "ohm", "merge integration", "part.part", "", false, true
		_, sourceOutput, err := createParameterTemplate(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateParameterTemplateInput{
			Name: sourceName, Units: &units, Description: &description, ModelType: &modelType, Choices: &choices, Checkbox: &checkbox, Enabled: &enabled,
		})
		r.NoError(err)
		r.NotNil(sourceOutput.Record)
		_, targetOutput, err := createParameterTemplate(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateParameterTemplateInput{
			Name: targetName, Units: &units, Description: &description, ModelType: &modelType, Choices: &choices, Checkbox: &checkbox, Enabled: &enabled,
		})
		r.NoError(err)
		r.NotNil(targetOutput.Record)
		updatedDescription := "explicit update integration"
		_, updatedOutput, err := updateParameterTemplate(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateInput{
			TemplateID: sourceOutput.Record.PK, Description: &updatedDescription, Checkbox: &checkbox,
		})
		r.NoError(err)
		r.NotNil(updatedOutput.Record)
		a.Equal(sourceName, updatedOutput.Record.Name, "omitted name must remain unchanged")
		r.NotNil(updatedOutput.Record.Units)
		a.Equal("ohm", *updatedOutput.Record.Units, "omitted units must remain unchanged")
		a.Equal(updatedDescription, updatedOutput.Record.Description)
		a.False(updatedOutput.Record.Checkbox, "explicit false must be preserved")
		parameter, err := fixture.client.CreatePartParameter(ctx, inventree.NewPartParameter(part.ID, sourceOutput.Record.PK, "10 ohm"))
		r.NoError(err)
		_, blockedDelete, err := deleteParameterTemplate(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeleteParameterTemplateInput{TemplateID: sourceOutput.Record.PK, Confirm: true})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, blockedDelete.Status)
		a.Equal([]int{parameter.PK}, blockedDelete.References.ParameterIDs)
		preservedTemplate, err := fixture.client.GetParameterTemplate(ctx, sourceOutput.Record.PK)
		r.NoError(err)
		a.Equal(sourceOutput.Record.PK, preservedTemplate.PK)
		preservedParameter, err := fixture.client.GetPartParameter(ctx, parameter.PK)
		r.NoError(err)
		a.Equal(sourceOutput.Record.PK, preservedParameter.Template)
		input := MergeParameterTemplatesInput{SourceTemplateID: sourceOutput.Record.PK, TargetTemplateID: targetOutput.Record.PK, ValueMap: map[string]string{"10 ohm": "12 ohm"}, DryRun: true}
		_, plan, err := mergeParameterTemplates(fixture.deps())(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		r.NotEmpty(plan.PlanHash)
		r.Len(plan.Actions, 1)
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = plan.PlanHash
		_, merged, err := mergeParameterTemplates(fixture.deps())(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusOK, merged.Status)
		a.True(merged.SourceDeleted)
		a.True(merged.ReadbackVerified)
		readback, err := fixture.client.GetPartParameter(ctx, parameter.PK)
		r.NoError(err)
		a.Equal(targetOutput.Record.PK, readback.Template)
		a.Equal("12 ohm", readback.Data)
		_, err = fixture.client.GetParameterTemplate(ctx, sourceOutput.Record.PK)
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		a.Equal(inventree.ErrorKindNotFound, apiErr.Kind)
	})

	t.Run("category_parameter_default_admin", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		parent := fixture.ensure(t, testenv.FixtureCategory)
		childName, err := fixture.run.Name("category-default-child")
		r.NoError(err)
		var child inventree.Category
		r.NoError(fixture.client.Post(ctx, "/api/part/category/", map[string]any{"name": childName, "description": "category-default inheritance child", "structural": false, "parent": parent.ID}, &child))
		r.NotZero(child.PK)
		templateName, err := fixture.run.Name("category-default-tool")
		r.NoError(err)
		template, err := fixture.client.CreateParameterTemplate(ctx, inventree.ParameterTemplateCreate{Name: templateName, Units: "", Description: "category default integration", ModelType: "part.part", Checkbox: false, Choices: "", Enabled: true})
		r.NoError(err)

		_, parentDefault, err := createCategoryParameterDefault(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateCategoryParameterDefaultInput{CategoryID: parent.ID, TemplateID: template.PK, DefaultValue: "parent"})
		r.NoError(err)
		_, created, err := createCategoryParameterDefault(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateCategoryParameterDefaultInput{CategoryID: child.PK, TemplateID: template.PK, DefaultValue: "child"})
		r.NoError(err)
		a.Equal(StatusOK, created.Status)
		r.NotNil(created.Record)
		a.Equal(child.PK, created.Record.CategoryID)
		a.Equal(template.PK, created.Record.TemplateID)

		_, listed, err := searchCategoryParameterDefaults(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{CategoryID: child.PK, TemplateID: template.PK})
		r.NoError(err)
		a.Equal(StatusOK, listed.Status)
		r.Len(listed.Records, 1)
		a.Equal(created.Record.LinkID, listed.Records[0].LinkID)
		a.False(listed.Records[0].Inherited)
		a.Equal("child", listed.Records[0].DefaultValue)

		_, effective, err := searchCategoryParameterDefaults(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchCategoryParameterDefaultsInput{CategoryID: child.PK, TemplateID: template.PK, IncludeParentDefaults: true})
		r.NoError(err)
		r.Len(effective.Records, 2)
		inheritedCount := 0
		for _, record := range effective.Records {
			if record.Inherited {
				inheritedCount++
				a.Equal(parent.ID, record.CategoryID)
				a.Equal("parent", record.DefaultValue)
			} else {
				a.Equal(child.PK, record.CategoryID)
				a.Equal("child", record.DefaultValue)
			}
		}
		a.Equal(1, inheritedCount)

		_, updated, err := updateCategoryParameterDefault(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateCategoryParameterDefaultInput{LinkID: created.Record.LinkID, DefaultValue: dvgoutils.Ptr("")})
		r.NoError(err)
		a.Equal(StatusOK, updated.Status)
		a.Empty(updated.Record.DefaultValue)

		_, confirmation, err := deleteCategoryParameterDefault(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeleteCategoryParameterDefaultInput{LinkID: created.Record.LinkID})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, confirmation.Status)
		_, deleted, err := deleteCategoryParameterDefault(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeleteCategoryParameterDefaultInput{LinkID: created.Record.LinkID, Confirm: true})
		r.NoError(err)
		a.Equal(StatusOK, deleted.Status)
		a.True(deleted.Verified)
		_, parentDeleted, err := deleteCategoryParameterDefault(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeleteCategoryParameterDefaultInput{LinkID: parentDefault.Record.LinkID, Confirm: true})
		r.NoError(err)
		a.True(parentDeleted.Verified)
	})

	t.Run("object_parameter_and_template_uniqueness", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		location := fixture.ensure(t, testenv.FixtureLocation)
		supplier := fixture.ensure(t, testenv.FixtureSupplier)
		secondLocationName, err := fixture.run.Name("object-parameter-second-location")
		r.NoError(err)
		secondLocation, err := fixture.client.CreateStockLocation(ctx, inventree.StockLocationCreate{Name: secondLocationName})
		r.NoError(err)
		templateName, err := fixture.run.Name("object-parameter-tool")
		r.NoError(err)
		units, description, modelType, choices, checkbox, enabled := "", "object-parameter integration", "", "", false, true
		unique := int(inventree.ParameterUniquenessModelType)
		_, templateOutput, err := createParameterTemplate(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateParameterTemplateInput{
			Name: templateName, Units: &units, Description: &description, ModelType: &modelType, Choices: &choices, Checkbox: &checkbox, Enabled: &enabled, Unique: &unique,
		})
		r.NoError(err)
		r.NotNil(templateOutput.Record)
		a.Equal(inventree.ParameterUniquenessModelType, templateOutput.Record.Unique)
		templateID := templateOutput.Record.PK

		_, created, err := createObjectParameter(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: location.ID, TemplateID: templateID, Value: dvgoutils.Ptr("bin-a")})
		r.NoError(err)
		a.Equal(StatusOK, created.Status)
		a.Equal("bin-a", created.Record.Value)
		parameterID := created.Record.ParameterID

		// A different object of the SAME model type sharing this value conflicts under unique:1 (model-type scope).
		_, conflict, err := createObjectParameter(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: secondLocation.PK, TemplateID: templateID, Value: dvgoutils.Ptr("bin-a")})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, conflict.Status)

		// The same value on a DIFFERENT model type does not conflict at unique:1 (model-type scope).
		_, companyCreated, err := createObjectParameter(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "company.company", ModelID: supplier.ID, TemplateID: templateID, Value: dvgoutils.Ptr("bin-a")})
		r.NoError(err)
		a.Equal(StatusOK, companyCreated.Status)

		// Re-calling create_object_parameter for the same object/template upserts in place instead of duplicating.
		_, updated, err := createObjectParameter(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "stock.stocklocation", ModelID: location.ID, TemplateID: templateID, Value: dvgoutils.Ptr("bin-b")})
		r.NoError(err)
		a.Equal(StatusOK, updated.Status)
		a.Equal(parameterID, updated.Record.ParameterID)
		a.Equal("bin-b", updated.Record.Value)

		_, listed, err := searchObjectParameterValues(fixture.deps())(ctx, &mcp.CallToolRequest{}, SearchObjectParametersInput{ModelType: "stock.stocklocation", ModelID: location.ID, TemplateID: templateID})
		r.NoError(err)
		a.Equal(StatusOK, listed.Status)
		r.Len(listed.Results, 1)
		a.Equal("bin-b", listed.Results[0].Value)

		// Attempting to tighten uniqueness to global now finds the cross-model-type "bin-a"/"bin-b" values do not conflict
		// (they differ), but flipping back to model-type scope after making them equal demonstrates the conflict guard.
		_, dryRun, err := updateParameterTemplateUniqueness(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: templateID, Unique: int(inventree.ParameterUniquenessGlobal), DryRun: true})
		r.NoError(err)
		a.Equal(StatusOK, dryRun.Status)
		a.Empty(dryRun.Conflicts)
		r.NotEmpty(dryRun.PlanHash)

		_, confirmed, err := updateParameterTemplateUniqueness(fixture.deps())(ctx, &mcp.CallToolRequest{}, UpdateParameterTemplateUniquenessInput{TemplateID: templateID, Unique: int(inventree.ParameterUniquenessGlobal), Confirm: true, PlanHash: dryRun.PlanHash})
		r.NoError(err)
		a.Equal(StatusOK, confirmed.Status)
		a.True(confirmed.Verified)
		a.Equal(inventree.ParameterUniquenessGlobal, confirmed.Record.Unique)

		// Now that unique is global, the same value across any model type conflicts.
		_, globalConflict, err := createObjectParameter(fixture.deps())(ctx, &mcp.CallToolRequest{}, CreateObjectParameterInput{ModelType: "company.company", ModelID: supplier.ID, TemplateID: templateID, Value: dvgoutils.Ptr("bin-b")})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, globalConflict.Status)

		_, preview, err := deleteObjectParameter(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeleteObjectParameterInput{ParameterID: parameterID})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, preview.Status)
		r.NotEmpty(preview.PlanHash)
		_, deleted, err := deleteObjectParameter(fixture.deps())(ctx, &mcp.CallToolRequest{}, DeleteObjectParameterInput{ParameterID: parameterID, Confirm: true, PlanHash: preview.PlanHash})
		r.NoError(err)
		a.Equal(StatusOK, deleted.Status)
		a.True(deleted.Verified)
		_, err = fixture.client.GetPartParameter(ctx, parameterID)
		var apiErr *inventree.APIError
		r.ErrorAs(err, &apiErr)
		a.Equal(inventree.ErrorKindNotFound, apiErr.Kind)
	})

	t.Run("bulk_parameter_propagation_and_audit", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		category := fixture.ensure(t, testenv.FixtureCategory)
		first := fixture.createPart(t, "bulk-parameter-first")
		second := fixture.createPart(t, "bulk-parameter-second")
		templateName, err := fixture.run.Name("bulk-parameter-template")
		r.NoError(err)
		template, err := fixture.client.CreateParameterTemplate(ctx, inventree.ParameterTemplateCreate{Name: templateName, Units: "", Description: "bulk propagation integration", ModelType: "part.part", Enabled: true})
		r.NoError(err)
		link, err := fixture.client.CreateCategoryParameterTemplate(ctx, inventree.CategoryParameterTemplateCreate{Category: category.ID, Template: template.PK, DefaultValue: "category-default"})
		r.NoError(err)
		existing, err := fixture.client.CreatePartParameter(ctx, inventree.NewPartParameter(first.PK, template.PK, "before"))
		r.NoError(err)
		value := "propagated"
		childName, err := fixture.run.Name("bulk-parameter-child-category")
		r.NoError(err)
		var childCategory inventree.Category
		r.NoError(fixture.client.Post(ctx, "/api/part/category/", map[string]any{"name": childName, "description": "bulk propagation descendant fixture", "parent": category.ID, "structural": false}, &childCategory))
		r.NotZero(childCategory.PK)
		location := fixture.ensure(t, testenv.FixtureLocation)
		childPartName, err := fixture.run.Name("bulk-parameter-child-part")
		r.NoError(err)
		childPart, err := fixture.client.CreatePart(ctx, inventree.PartCreate{Name: childPartName, Category: dvgoutils.Ptr(childCategory.PK), DefaultLocation: dvgoutils.Ptr(location.ID), Active: dvgoutils.Ptr(true), Component: dvgoutils.Ptr(true)})
		r.NoError(err)
		childRowsBefore, err := fixture.client.SearchPartParameters(ctx, inventree.PartParameterQuery{PartID: childPart.PK, TemplateID: template.PK})
		r.NoError(err)

		_, exactCategory, err := bulkPropagatePartParameters(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkPropagatePartParametersInput{DryRun: true, TemplateID: template.PK, Value: &value, CategoryID: category.ID})
		r.NoError(err)
		r.Len(exactCategory.Plan.Actions, 2)
		a.Equal([]int{first.PK, second.PK}, exactCategory.Plan.PartIDs)
		_, descendantCategory, err := bulkPropagatePartParameters(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkPropagatePartParametersInput{DryRun: true, TemplateID: template.PK, Value: &value, CategoryID: category.ID, IncludeSubcategories: true})
		r.NoError(err)
		r.Len(descendantCategory.Plan.Actions, 3)
		a.Contains(descendantCategory.Plan.PartIDs, childPart.PK)
		childRows, err := fixture.client.SearchPartParameters(ctx, inventree.PartParameterQuery{PartID: childPart.PK, TemplateID: template.PK})
		r.NoError(err)
		a.Equal(childRowsBefore, childRows, "category selector dry runs must not change rows")

		input := BulkPropagatePartParametersInput{DryRun: true, TemplateID: template.PK, Value: &value, PartIDs: []int{second.PK, first.PK}, OverwriteExisting: true}

		_, plan, err := bulkPropagatePartParameters(fixture.deps())(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusOK, plan.Status)
		r.NotEmpty(plan.PlanHash)
		r.Len(plan.Plan.Actions, 2)
		a.Equal("update", plan.Plan.Actions[0].Action)
		a.Equal("create", plan.Plan.Actions[1].Action)

		input.DryRun = false
		input.Confirm = true
		input.PlanHash = plan.PlanHash
		_, executed, err := bulkPropagatePartParameters(fixture.deps())(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusOK, executed.Status)
		a.Equal(2, executed.Applied)
		a.Zero(executed.Failed)
		updated, err := fixture.client.GetPartParameter(ctx, existing.PK)
		r.NoError(err)
		a.Equal(value, updated.Data)
		secondRows, err := fixture.client.SearchPartParameters(ctx, inventree.PartParameterQuery{PartID: second.PK, TemplateID: template.PK})
		r.NoError(err)
		r.Len(secondRows, 1)
		a.Equal(value, secondRows[0].Data)

		_, audit, err := auditParameterConsistency(fixture.deps())(ctx, &mcp.CallToolRequest{}, AuditParameterConsistencyInput{TemplateID: template.PK, CategoryID: category.ID})
		r.NoError(err)
		a.Equal(StatusOK, audit.Status)
		a.Equal(2, audit.RowsRead)
		mismatches := 0
		for _, finding := range audit.Findings {
			if finding.Kind == "category_default_mismatch" {
				mismatches++
			}
		}
		a.Equal(2, mismatches)

		r.NoError(fixture.client.DeletePartParameter(ctx, existing.PK))
		r.NoError(fixture.client.DeletePartParameter(ctx, secondRows[0].PK))
		r.NoError(fixture.client.DeleteCategoryParameterTemplate(ctx, link.PK))
		r.NoError(fixture.client.DeleteParameterTemplate(ctx, template.PK))
	})

	t.Run("bulk_catalog_and_company_updates", func(t *testing.T) {
		unknownID := 999999999

		t.Run("parts", func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fixture := newMilestoneToolFixture(t, shared)
			applies := fixture.createPart(t, "bulk-part-applies")
			skips := fixture.createPart(t, "bulk-part-skips")
			description := "bulk_update_parts integration"

			items := []BulkUpdatePartItem{
				{ID: applies.PK, Description: &description},
				{ID: skips.PK, Active: dvgoutils.Ptr(true)}, // already active: no-op
				{ID: unknownID, Description: &description},
			}
			_, dryRun, err := bulkUpdateParts(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, DryRun: true})
			r.NoError(err)
			a.Equal(StatusOK, dryRun.Status)
			r.NotEmpty(dryRun.PlanHash)

			_, confirmed, err := bulkUpdateParts(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePartsInput{Items: items, Confirm: true, PlanHash: dryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusPartialFailure, confirmed.Status)
			byID := bulkResultsByID(confirmed.Items)
			r.NotNil(byID[applies.PK].Record)
			a.Equal(description, byID[applies.PK].Record.Description)
			a.Equal(string(batch.OutcomeApplied), byID[applies.PK].Outcome)
			a.Equal(string(batch.OutcomeSkipped), byID[skips.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[unknownID].Outcome)

			refreshed, err := fixture.client.GetPartDetail(ctx, applies.PK)
			r.NoError(err)
			a.Equal(description, refreshed.Description)
		})

		t.Run("companies", func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fixture := newMilestoneToolFixture(t, shared)
			appliesName, err := fixture.run.Name("bulk-company-applies")
			r.NoError(err)
			applies, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{Name: appliesName, Currency: "USD", IsSupplier: true})
			r.NoError(err)
			skipsName, err := fixture.run.Name("bulk-company-skips")
			r.NoError(err)
			skips, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{Name: skipsName, Currency: "USD"})
			r.NoError(err)
			description := "bulk_update_companies integration"

			items := []BulkUpdateCompanyItem{
				{ID: applies.PK, Description: &description},
				{ID: skips.PK, Name: &skipsName}, // unchanged: no-op
				{ID: unknownID, Description: &description},
			}
			_, dryRun, err := bulkUpdateCompanies(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: items, DryRun: true})
			r.NoError(err)
			r.NotEmpty(dryRun.PlanHash)

			_, confirmed, err := bulkUpdateCompanies(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateCompaniesInput{Items: items, Confirm: true, PlanHash: dryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusPartialFailure, confirmed.Status)
			byID := bulkResultsByID(confirmed.Items)
			r.NotNil(byID[applies.PK].Record)
			a.Equal(description, byID[applies.PK].Record.Description)
			a.Equal(string(batch.OutcomeApplied), byID[applies.PK].Outcome)
			a.Equal(string(batch.OutcomeSkipped), byID[skips.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[unknownID].Outcome)

			// Role removal is explicitly out of scope for the bulk tool.
			explicitFalse := false
			plan := buildCompanyBulkPlan(ctx, fixture.client, []BulkUpdateCompanyItem{{ID: applies.PK, IsSupplier: &explicitFalse}})
			r.Len(plan.Items, 1)
			a.NotEmpty(plan.Items[0].FailReason)
		})

		t.Run("part_categories", func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fixture := newMilestoneToolFixture(t, shared)
			appliesName, err := fixture.run.Name("bulk-category-applies")
			r.NoError(err)
			applies, err := fixture.client.CreatePartCategory(ctx, inventree.CategoryCreate{Name: appliesName})
			r.NoError(err)
			skipsName, err := fixture.run.Name("bulk-category-skips")
			r.NoError(err)
			skips, err := fixture.client.CreatePartCategory(ctx, inventree.CategoryCreate{Name: skipsName})
			r.NoError(err)
			description := "bulk_update_part_categories integration"

			items := []BulkUpdatePartCategoryItem{
				{ID: applies.PK, Description: &description},
				{ID: skips.PK, Name: &skipsName}, // unchanged: no-op
				{ID: unknownID, Description: &description},
			}
			_, dryRun, err := bulkUpdatePartCategories(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePartCategoriesInput{Items: items, DryRun: true})
			r.NoError(err)
			r.NotEmpty(dryRun.PlanHash)

			_, confirmed, err := bulkUpdatePartCategories(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePartCategoriesInput{Items: items, Confirm: true, PlanHash: dryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusPartialFailure, confirmed.Status)
			byID := bulkResultsByID(confirmed.Items)
			r.NotNil(byID[applies.PK].Record)
			a.Equal(description, byID[applies.PK].Record.Description)
			a.Equal(string(batch.OutcomeApplied), byID[applies.PK].Outcome)
			a.Equal(string(batch.OutcomeSkipped), byID[skips.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[unknownID].Outcome)
		})

		t.Run("supplier_parts", func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fixture := newMilestoneToolFixture(t, shared)
			part := fixture.createPart(t, "bulk-supplierpart-base")
			supplier := fixture.ensure(t, testenv.FixtureSupplier)
			appliesSKU, err := fixture.run.Name("bulk-supplierpart-applies")
			r.NoError(err)
			applies, err := fixture.client.CreateSupplierPart(ctx, inventree.SupplierPartCreate{Part: part.PK, Supplier: supplier.ID, SKU: appliesSKU})
			r.NoError(err)
			skipsSKU, err := fixture.run.Name("bulk-supplierpart-skips")
			r.NoError(err)
			skips, err := fixture.client.CreateSupplierPart(ctx, inventree.SupplierPartCreate{Part: part.PK, Supplier: supplier.ID, SKU: skipsSKU})
			r.NoError(err)
			packaging := "bulk-reel"

			items := []BulkUpdateSupplierPartItem{
				{ID: applies.PK, Packaging: &packaging},
				{ID: skips.PK, SKU: &skipsSKU}, // unchanged: no-op
				{ID: unknownID, Packaging: &packaging},
			}
			_, dryRun, err := bulkUpdateSupplierParts(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateSupplierPartsInput{Items: items, DryRun: true})
			r.NoError(err)
			r.NotEmpty(dryRun.PlanHash)

			_, confirmed, err := bulkUpdateSupplierParts(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateSupplierPartsInput{Items: items, Confirm: true, PlanHash: dryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusPartialFailure, confirmed.Status)
			byID := bulkResultsByID(confirmed.Items)
			r.NotNil(byID[applies.PK].Record)
			r.NotNil(byID[applies.PK].Record.Packaging)
			a.Equal(packaging, *byID[applies.PK].Record.Packaging)
			a.Equal(string(batch.OutcomeApplied), byID[applies.PK].Outcome)
			a.Equal(string(batch.OutcomeSkipped), byID[skips.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[unknownID].Outcome)
		})

		t.Run("manufacturer_parts", func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fixture := newMilestoneToolFixture(t, shared)
			part := fixture.createPart(t, "bulk-manufacturerpart-base")
			manufacturer := fixture.ensure(t, testenv.FixtureManufacturer)
			appliesMPN, err := fixture.run.Name("bulk-mfgpart-applies")
			r.NoError(err)
			applies, err := fixture.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{Part: part.PK, Manufacturer: manufacturer.ID, MPN: &appliesMPN})
			r.NoError(err)
			skipsMPN, err := fixture.run.Name("bulk-mfgpart-skips")
			r.NoError(err)
			skips, err := fixture.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{Part: part.PK, Manufacturer: manufacturer.ID, MPN: &skipsMPN})
			r.NoError(err)
			description := "bulk_update_manufacturer_parts integration"

			items := []BulkUpdateManufacturerPartItem{
				{ID: applies.PK, Description: &description},
				{ID: skips.PK, MPN: &skipsMPN}, // unchanged: no-op
				{ID: unknownID, Description: &description},
			}
			_, dryRun, err := bulkUpdateManufacturerParts(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateManufacturerPartsInput{Items: items, DryRun: true})
			r.NoError(err)
			r.NotEmpty(dryRun.PlanHash)

			_, confirmed, err := bulkUpdateManufacturerParts(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateManufacturerPartsInput{Items: items, Confirm: true, PlanHash: dryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusPartialFailure, confirmed.Status)
			byID := bulkResultsByID(confirmed.Items)
			r.NotNil(byID[applies.PK].Record)
			r.NotNil(byID[applies.PK].Record.Description)
			a.Equal(description, *byID[applies.PK].Record.Description)
			a.Equal(string(batch.OutcomeApplied), byID[applies.PK].Outcome)
			a.Equal(string(batch.OutcomeSkipped), byID[skips.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[unknownID].Outcome)
		})
	})

	t.Run("bulk_stock_updates", func(t *testing.T) {
		unknownID := 999999999

		t.Run("metadata", func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fixture := newMilestoneToolFixture(t, shared)
			part := fixture.createPart(t, "bulk-stock-metadata-base")
			location := fixture.ensure(t, testenv.FixtureLocation)
			applies := fixture.createStockItem(t, part.PK, location.ID)
			skips := fixture.createStockItem(t, part.PK, location.ID)
			_, err := fixture.client.UpdateStockItem(ctx, skips.PK, inventree.PatchFields{"packaging": inventree.Set("already-set")})
			r.NoError(err)
			batchValue := "bulk-metadata-applied"

			items := []BulkUpdateStockItemMetadataItem{
				{ID: applies.PK, Batch: &batchValue},
				{ID: skips.PK, Packaging: dvgoutils.Ptr("already-set")}, // matches current: no-op
				{ID: unknownID, Batch: &batchValue},
			}
			_, dryRun, err := bulkUpdateStockItemMetadata(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: items, DryRun: true})
			r.NoError(err)
			a.Equal(StatusOK, dryRun.Status)
			r.NotEmpty(dryRun.PlanHash)

			_, confirmed, err := bulkUpdateStockItemMetadata(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: items, Confirm: true, PlanHash: dryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusPartialFailure, confirmed.Status)
			byID := bulkResultsByID(confirmed.Items)
			r.NotNil(byID[applies.PK].Record)
			r.NotNil(byID[applies.PK].Record.Batch)
			a.Equal(batchValue, *byID[applies.PK].Record.Batch)
			a.Equal(string(batch.OutcomeApplied), byID[applies.PK].Outcome)
			a.Equal(string(batch.OutcomeSkipped), byID[skips.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[unknownID].Outcome)

			refreshed, err := fixture.client.GetStockItem(ctx, applies.PK)
			r.NoError(err)
			r.NotNil(refreshed.Batch)
			a.Equal(batchValue, *refreshed.Batch)

			// Stale plan: state drifts after the dry run but before confirm, so
			// the digest embedded in the freshly rebuilt plan at confirm time no
			// longer matches and the stored token must be rejected.
			staleItems := []BulkUpdateStockItemMetadataItem{{ID: applies.PK, Packaging: dvgoutils.Ptr("bulk-stale-packaging")}}
			_, staleDryRun, err := bulkUpdateStockItemMetadata(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: staleItems, DryRun: true})
			r.NoError(err)
			r.NotEmpty(staleDryRun.PlanHash)
			_, err = fixture.client.UpdateStockItem(ctx, applies.PK, inventree.PatchFields{"packaging": inventree.Set("someone-else-changed-it")})
			r.NoError(err)
			_, staleConfirm, err := bulkUpdateStockItemMetadata(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: staleItems, Confirm: true, PlanHash: staleDryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, staleConfirm.Status)

			// Response-loss recovery: the live PATCH lands upstream but the
			// response is dropped, so Mutate must recover by reading back
			// current state rather than reporting a bare ambiguous failure.
			lostItems := []BulkUpdateStockItemMetadataItem{{ID: applies.PK, Notes: dvgoutils.Ptr("bulk-recovered-notes")}}
			_, lostDryRun, err := bulkUpdateStockItemMetadata(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: lostItems, DryRun: true})
			r.NoError(err)
			r.NotEmpty(lostDryRun.PlanHash)
			lostClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPatch, path: fmt.Sprintf("/api/stock/%d/", applies.PK)}})
			r.NoError(err)
			lostFixture := fixture
			lostFixture.client = lostClient
			_, lostConfirm, err := bulkUpdateStockItemMetadata(lostFixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: lostItems, Confirm: true, PlanHash: lostDryRun.PlanHash})
			r.NoError(err)
			r.Len(lostConfirm.Items, 1)
			a.Equal(string(batch.OutcomeApplied), lostConfirm.Items[0].Outcome)
			r.NotNil(lostConfirm.Items[0].Record)
			r.NotNil(lostConfirm.Items[0].Record.Notes)
			a.Equal("bulk-recovered-notes", *lostConfirm.Items[0].Record.Notes)
		})

		t.Run("status", func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fixture := newMilestoneToolFixture(t, shared)
			part := fixture.createPart(t, "bulk-stock-status-base")
			location := fixture.ensure(t, testenv.FixtureLocation)
			applies := fixture.createStockItem(t, part.PK, location.ID)
			skips, err := fixture.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.PK, Location: location.ID, Quantity: 3, Status: dvgoutils.Ptr(stockStatusAttention)})
			r.NoError(err)
			highRisk := fixture.createStockItem(t, part.PK, location.ID)

			items := []BulkSetStockStatusItem{
				{ID: applies.PK, Status: stockStatusAttention},
				{ID: skips.PK, Status: stockStatusAttention}, // already attention: no-op
				{ID: unknownID, Status: stockStatusAttention},
				{ID: highRisk.PK, Status: stockStatusDestroyed}, // high-risk target: refused
			}
			_, dryRun, err := bulkSetStockStatus(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "bulk stock status integration", DryRun: true})
			r.NoError(err)
			a.Equal(StatusOK, dryRun.Status)
			r.NotEmpty(dryRun.PlanHash)

			_, confirmed, err := bulkSetStockStatus(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "bulk stock status integration", Confirm: true, PlanHash: dryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusPartialFailure, confirmed.Status)
			byID := bulkResultsByID(confirmed.Items)
			r.NotNil(byID[applies.PK].Record)
			a.Equal(stockStatusAttention, byID[applies.PK].Record.Status)
			a.Equal(string(batch.OutcomeApplied), byID[applies.PK].Outcome)
			a.Equal(string(batch.OutcomeSkipped), byID[skips.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[unknownID].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[highRisk.PK].Outcome)

			refreshed, err := fixture.client.GetStockItem(ctx, applies.PK)
			r.NoError(err)
			a.Equal(stockStatusAttention, refreshed.Status)
			unchanged, err := fixture.client.GetStockItem(ctx, highRisk.PK)
			r.NoError(err)
			a.Equal(stockStatusOK, unchanged.Status)

			// Stale plan: state drifts after the dry run but before confirm.
			staleItems := []BulkSetStockStatusItem{{ID: applies.PK, Status: stockStatusQuarantine}}
			_, staleDryRun, err := bulkSetStockStatus(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: staleItems, Reason: "stale check", DryRun: true})
			r.NoError(err)
			r.NotEmpty(staleDryRun.PlanHash)
			r.NoError(fixture.client.ChangeStockStatus(ctx, inventree.StockStatusChange{Items: []int{applies.PK}, Status: stockStatusDamaged, Note: "drift"}))
			_, staleConfirm, err := bulkSetStockStatus(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: staleItems, Reason: "stale check", Confirm: true, PlanHash: staleDryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, staleConfirm.Status)

			// Response-loss recovery: the live status-change call lands upstream
			// but the response is dropped, so Mutate must recover by reading
			// back current state rather than reporting a bare ambiguous failure.
			lostItems := []BulkSetStockStatusItem{{ID: applies.PK, Status: stockStatusReturned}}
			_, lostDryRun, err := bulkSetStockStatus(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: lostItems, Reason: "recovered", DryRun: true})
			r.NoError(err)
			r.NotEmpty(lostDryRun.PlanHash)
			lostClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPost, path: "/api/stock/change_status/"}})
			r.NoError(err)
			lostFixture := fixture
			lostFixture.client = lostClient
			_, lostConfirm, err := bulkSetStockStatus(lostFixture.deps())(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: lostItems, Reason: "recovered", Confirm: true, PlanHash: lostDryRun.PlanHash})
			r.NoError(err)
			r.Len(lostConfirm.Items, 1)
			a.Equal(string(batch.OutcomeApplied), lostConfirm.Items[0].Outcome)
			r.NotNil(lostConfirm.Items[0].Record)
			a.Equal(stockStatusReturned, lostConfirm.Items[0].Record.Status)
		})
	})

	t.Run("bulk_purchase_order_updates", func(t *testing.T) {
		unknownID := 999999999

		t.Run("orders", func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fixture := newMilestoneToolFixture(t, shared)
			supplier := fixture.ensure(t, testenv.FixtureSupplier)

			applies, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID})
			r.NoError(err)
			skips, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID, Description: dvgoutils.Ptr("already-set")})
			r.NoError(err)
			newDescription := "bulk-po-applied"

			items := []BulkUpdatePurchaseOrderItem{
				{ID: applies.PK, Description: &newDescription},
				{ID: skips.PK, Description: dvgoutils.Ptr("already-set")}, // matches current: no-op
				{ID: unknownID, Description: &newDescription},
			}
			_, dryRun, err := bulkUpdatePurchaseOrders(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: items, DryRun: true})
			r.NoError(err)
			a.Equal(StatusOK, dryRun.Status)
			r.NotEmpty(dryRun.PlanHash)

			_, confirmed, err := bulkUpdatePurchaseOrders(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: items, Confirm: true, PlanHash: dryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusPartialFailure, confirmed.Status)
			byID := bulkResultsByID(confirmed.Items)
			r.NotNil(byID[applies.PK].Record)
			a.Equal(newDescription, byID[applies.PK].Record.Description)
			a.Equal(string(batch.OutcomeApplied), byID[applies.PK].Outcome)
			a.Equal(string(batch.OutcomeSkipped), byID[skips.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[unknownID].Outcome)

			refreshed, err := fixture.client.GetPurchaseOrder(ctx, applies.PK)
			r.NoError(err)
			a.Equal(newDescription, refreshed.Description)

			// Stale plan: state drifts after the dry run but before confirm, so
			// the digest embedded in the freshly rebuilt plan at confirm time no
			// longer matches and the stored token must be rejected.
			staleItems := []BulkUpdatePurchaseOrderItem{{ID: applies.PK, Description: dvgoutils.Ptr("bulk-stale-description")}}
			_, staleDryRun, err := bulkUpdatePurchaseOrders(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: staleItems, DryRun: true})
			r.NoError(err)
			r.NotEmpty(staleDryRun.PlanHash)
			_, err = fixture.client.UpdatePurchaseOrderDetail(ctx, applies.PK, inventree.PatchFields{"description": inventree.Set("someone-else-changed-it")})
			r.NoError(err)
			_, staleConfirm, err := bulkUpdatePurchaseOrders(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: staleItems, Confirm: true, PlanHash: staleDryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, staleConfirm.Status)

			// Response-loss recovery: the live PATCH lands upstream but the
			// response is dropped, so Mutate must recover by reading back
			// current state rather than reporting a bare ambiguous failure.
			lostItems := []BulkUpdatePurchaseOrderItem{{ID: applies.PK, Notes: dvgoutils.Ptr("bulk-recovered-notes")}}
			_, lostDryRun, err := bulkUpdatePurchaseOrders(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: lostItems, DryRun: true})
			r.NoError(err)
			r.NotEmpty(lostDryRun.PlanHash)
			lostClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPatch, path: fmt.Sprintf("/api/order/po/%d/", applies.PK)}})
			r.NoError(err)
			lostFixture := fixture
			lostFixture.client = lostClient
			_, lostConfirm, err := bulkUpdatePurchaseOrders(lostFixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrdersInput{Items: lostItems, Confirm: true, PlanHash: lostDryRun.PlanHash})
			r.NoError(err)
			r.Len(lostConfirm.Items, 1)
			a.Equal(string(batch.OutcomeApplied), lostConfirm.Items[0].Outcome)
			r.NotNil(lostConfirm.Items[0].Record)
			r.NotNil(lostConfirm.Items[0].Record.Notes)
			a.Equal("bulk-recovered-notes", *lostConfirm.Items[0].Record.Notes)
		})

		t.Run("lines", func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fixture := newMilestoneToolFixture(t, shared)
			supplier := fixture.ensure(t, testenv.FixtureSupplier)
			supplierPartFixture := fixture.ensure(t, testenv.FixtureSupplierPart)
			supplierPart, err := fixture.client.GetSupplierPart(ctx, supplierPartFixture.ID)
			r.NoError(err)
			otherSupplierName, err := fixture.run.Name("bulk-line-other-supplier")
			r.NoError(err)
			otherSupplier, err := fixture.client.CreateCompany(ctx, inventree.CompanyCreate{Name: otherSupplierName, Currency: "AUD", IsSupplier: true})
			r.NoError(err)
			otherSKU, err := fixture.run.Name("bulk-line-other-sku")
			r.NoError(err)
			otherSupplierPart, err := fixture.client.CreateSupplierPart(ctx, inventree.SupplierPartCreate{Part: supplierPart.Part, Supplier: otherSupplier.PK, SKU: otherSKU})
			r.NoError(err)

			order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID})
			r.NoError(err)
			appliesReference, err := fixture.run.Name("bulk-line-applies")
			r.NoError(err)
			applies, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPartFixture.ID, Reference: &appliesReference, Quantity: 1})
			r.NoError(err)
			skipsReference, err := fixture.run.Name("bulk-line-skips")
			r.NoError(err)
			skips, err := fixture.client.CreatePurchaseOrderLine(ctx, inventree.PurchaseOrderLineCreate{Order: order.PK, SupplierPart: supplierPartFixture.ID, Reference: &skipsReference, Quantity: 3})
			r.NoError(err)

			items := []BulkUpdatePurchaseOrderLineItem{
				{ID: applies.PK, Quantity: dvgoutils.Ptr(5.0)},
				{ID: skips.PK, Quantity: dvgoutils.Ptr(3.0)}, // matches current: no-op
				{ID: unknownID, Quantity: dvgoutils.Ptr(5.0)},
			}
			_, dryRun, err := bulkUpdatePurchaseOrderLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: items, DryRun: true})
			r.NoError(err)
			a.Equal(StatusOK, dryRun.Status)
			r.NotEmpty(dryRun.PlanHash)

			_, confirmed, err := bulkUpdatePurchaseOrderLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: items, Confirm: true, PlanHash: dryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusPartialFailure, confirmed.Status)
			byID := bulkResultsByID(confirmed.Items)
			r.NotNil(byID[applies.PK].Record)
			a.InDelta(5.0, byID[applies.PK].Record.Quantity, 1e-9)
			a.Equal(string(batch.OutcomeApplied), byID[applies.PK].Outcome)
			a.Equal(string(batch.OutcomeSkipped), byID[skips.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[unknownID].Outcome)

			refreshed, err := fixture.client.GetPurchaseOrderLine(ctx, applies.PK)
			r.NoError(err)
			a.InDelta(5.0, refreshed.Quantity, 1e-9)

			// Supplier consistency: replacing supplier_part_id with one that
			// belongs to a different supplier than the order must be rejected
			// without a write, matching update_purchase_order_line's own check.
			mismatchItems := []BulkUpdatePurchaseOrderLineItem{{ID: applies.PK, SupplierPartID: &otherSupplierPart.PK}}
			_, mismatchDryRun, err := bulkUpdatePurchaseOrderLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: mismatchItems, DryRun: true})
			r.NoError(err)
			r.Len(mismatchDryRun.Items, 1)
			a.Equal(bulkOutcomeFailed, mismatchDryRun.Items[0].Outcome)
			a.Contains(mismatchDryRun.Items[0].Message, "does not belong to the purchase-order supplier")

			// Response-loss recovery.
			lostItems := []BulkUpdatePurchaseOrderLineItem{{ID: applies.PK, Notes: dvgoutils.Ptr("bulk-recovered-notes")}}
			_, lostDryRun, err := bulkUpdatePurchaseOrderLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: lostItems, DryRun: true})
			r.NoError(err)
			r.NotEmpty(lostDryRun.PlanHash)
			lostClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPatch, path: fmt.Sprintf("/api/order/po-line/%d/", applies.PK)}})
			r.NoError(err)
			lostFixture := fixture
			lostFixture.client = lostClient
			_, lostConfirm, err := bulkUpdatePurchaseOrderLines(lostFixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderLinesInput{Items: lostItems, Confirm: true, PlanHash: lostDryRun.PlanHash})
			r.NoError(err)
			r.Len(lostConfirm.Items, 1)
			a.Equal(string(batch.OutcomeApplied), lostConfirm.Items[0].Outcome)
			r.NotNil(lostConfirm.Items[0].Record)
			a.Equal("bulk-recovered-notes", lostConfirm.Items[0].Record.Notes)
		})

		t.Run("extra_lines", func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fixture := newMilestoneToolFixture(t, shared)
			supplier := fixture.ensure(t, testenv.FixtureSupplier)
			order, err := fixture.client.CreatePurchaseOrder(ctx, inventree.PurchaseOrderCreate{Supplier: supplier.ID})
			r.NoError(err)

			appliesReference, err := fixture.run.Name("bulk-extra-applies")
			r.NoError(err)
			applies, err := fixture.client.CreatePurchaseOrderExtraLine(ctx, inventree.PurchaseOrderExtraLineCreate{Order: order.PK, Reference: appliesReference, Quantity: 1})
			r.NoError(err)
			skipsReference, err := fixture.run.Name("bulk-extra-skips")
			r.NoError(err)
			skips, err := fixture.client.CreatePurchaseOrderExtraLine(ctx, inventree.PurchaseOrderExtraLineCreate{Order: order.PK, Reference: skipsReference, Description: dvgoutils.Ptr("already-set"), Quantity: 1})
			r.NoError(err)
			takenReference, err := fixture.run.Name("bulk-extra-taken")
			r.NoError(err)
			_, err = fixture.client.CreatePurchaseOrderExtraLine(ctx, inventree.PurchaseOrderExtraLineCreate{Order: order.PK, Reference: takenReference, Quantity: 1})
			r.NoError(err)
			collidesReference, err := fixture.run.Name("bulk-extra-collides")
			r.NoError(err)
			collides, err := fixture.client.CreatePurchaseOrderExtraLine(ctx, inventree.PurchaseOrderExtraLineCreate{Order: order.PK, Reference: collidesReference, Quantity: 1})
			r.NoError(err)
			batchCollideAReference, err := fixture.run.Name("bulk-extra-batch-collide-a")
			r.NoError(err)
			batchCollideA, err := fixture.client.CreatePurchaseOrderExtraLine(ctx, inventree.PurchaseOrderExtraLineCreate{Order: order.PK, Reference: batchCollideAReference, Quantity: 1})
			r.NoError(err)
			batchCollideBReference, err := fixture.run.Name("bulk-extra-batch-collide-b")
			r.NoError(err)
			batchCollideB, err := fixture.client.CreatePurchaseOrderExtraLine(ctx, inventree.PurchaseOrderExtraLineCreate{Order: order.PK, Reference: batchCollideBReference, Quantity: 1})
			r.NoError(err)
			sharedNewReference, err := fixture.run.Name("bulk-extra-shared-new")
			r.NoError(err)
			newDescription := "bulk-extra-applied"

			items := []BulkUpdatePurchaseOrderExtraLineItem{
				{ID: applies.PK, Description: &newDescription},
				{ID: skips.PK, Description: dvgoutils.Ptr("already-set")}, // matches current: no-op
				{ID: unknownID, Description: &newDescription},
				{ID: collides.PK, Reference: &takenReference}, // reference already used by another extra line on this order
				// Two batch items independently requesting the same brand-new
				// reference: neither exists upstream yet, so only cross-item
				// in-batch detection (not the upstream duplicate scan) can
				// catch this collision.
				{ID: batchCollideA.PK, Reference: &sharedNewReference},
				{ID: batchCollideB.PK, Reference: &sharedNewReference},
			}
			_, dryRun, err := bulkUpdatePurchaseOrderExtraLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderExtraLinesInput{Items: items, DryRun: true})
			r.NoError(err)
			a.Equal(StatusOK, dryRun.Status)
			r.NotEmpty(dryRun.PlanHash)
			byIDDry := bulkResultsByID(dryRun.Items)
			a.Equal(bulkOutcomeFailed, byIDDry[collides.PK].Outcome)
			a.Contains(byIDDry[collides.PK].Message, "collides with an existing extra line")
			a.Equal(bulkOutcomeFailed, byIDDry[batchCollideA.PK].Outcome)
			a.Equal(bulkReasonDuplicateExtraLineReference, byIDDry[batchCollideA.PK].Message)
			a.Equal(bulkOutcomeFailed, byIDDry[batchCollideB.PK].Outcome)
			a.Equal(bulkReasonDuplicateExtraLineReference, byIDDry[batchCollideB.PK].Message)

			_, confirmed, err := bulkUpdatePurchaseOrderExtraLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderExtraLinesInput{Items: items, Confirm: true, PlanHash: dryRun.PlanHash})
			r.NoError(err)
			a.Equal(StatusPartialFailure, confirmed.Status)
			byID := bulkResultsByID(confirmed.Items)
			r.NotNil(byID[applies.PK].Record)
			a.Equal(newDescription, byID[applies.PK].Record.Description)
			a.Equal(string(batch.OutcomeApplied), byID[applies.PK].Outcome)
			a.Equal(string(batch.OutcomeSkipped), byID[skips.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[unknownID].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[collides.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[batchCollideA.PK].Outcome)
			a.Equal(string(batch.OutcomeFailed), byID[batchCollideB.PK].Outcome)

			refreshed, err := fixture.client.GetPurchaseOrderExtraLine(ctx, applies.PK)
			r.NoError(err)
			a.Equal(newDescription, refreshed.Description)

			// Response-loss recovery.
			lostItems := []BulkUpdatePurchaseOrderExtraLineItem{{ID: applies.PK, Notes: dvgoutils.Ptr("bulk-recovered-notes")}}
			_, lostDryRun, err := bulkUpdatePurchaseOrderExtraLines(fixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderExtraLinesInput{Items: lostItems, DryRun: true})
			r.NoError(err)
			r.NotEmpty(lostDryRun.PlanHash)
			lostClient, err := shared.ClientWithHTTPClient(fixture.account, &http.Client{Transport: &loseMutationResponseTransport{base: http.DefaultTransport, method: http.MethodPatch, path: fmt.Sprintf("/api/order/po-extra-line/%d/", applies.PK)}})
			r.NoError(err)
			lostFixture := fixture
			lostFixture.client = lostClient
			_, lostConfirm, err := bulkUpdatePurchaseOrderExtraLines(lostFixture.deps())(ctx, &mcp.CallToolRequest{}, BulkUpdatePurchaseOrderExtraLinesInput{Items: lostItems, Confirm: true, PlanHash: lostDryRun.PlanHash})
			r.NoError(err)
			r.Len(lostConfirm.Items, 1)
			a.Equal(string(batch.OutcomeApplied), lostConfirm.Items[0].Outcome)
			r.NotNil(lostConfirm.Items[0].Record)
			a.Equal("bulk-recovered-notes", lostConfirm.Items[0].Record.Notes)
		})
	})

	t.Run("deferred_file_surface_boundaries_return_clarifications", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fixture := newMilestoneToolFixture(t, shared)
		for _, modelType := range []string{"salesorder", "salesordershipment", "returnorder", "transferorder", "build"} {
			_, output, err := uploadAttachment(fixture.deps())(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
				ModelType:    modelType,
				ModelID:      1,
				Filename:     modelType + ".txt",
				ContentType:  "text/plain",
				InlineBase64: base64.StdEncoding.EncodeToString([]byte("deferred")),
			})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, output.Status)
			r.NotNil(output.Clarification)
			a.Equal("model_type", output.Clarification.Retry)
			a.Contains(output.Clarification.Reason, `model type "`+modelType+`" is out of scope`)
		}
		a.NotContains(ToolAuthorizations, "notes_image_upload")
		a.NotContains(ToolAuthorizations, "upload_report_attachment")
		a.NotContains(ToolAuthorizations, "upload_stock_test_result_attachment")
	})
}

type milestoneToolFixture struct {
	shared                               *testenv.SharedInvenTree
	run                                  *testenv.Run
	account                              *testenv.Account
	client                               *inventree.Client
	stockPlanStore                       *stockPlanStore
	stockProvenancePlanStore             *stockProvenancePlanStore
	parameterPlanStore                   *parameterPlanStore
	partFamilyPlanStore                  *partFamilyPlanStore
	partRelationPlanStore                *partRelationPlanStore
	companyRolePlanStore                 *companyRolePlanStore
	ownerPlanStore                       *ownerPlanStore
	contactPlanStore                     *contactPlanStore
	addressPlanStore                     *addressPlanStore
	projectCodePlanStore                 *projectCodePlanStore
	objectParameterDeletePlanStore       *objectParameterDeletePlanStore
	parameterTemplateUniquenessPlanStore *parameterTemplateUniquenessPlanStore
	purchaseOrderLifecyclePlanStore      *purchaseOrderLifecyclePlanStore
	partBulkPlanStore                    *batch.Store[partBulkPlan]
	companyBulkPlanStore                 *batch.Store[companyBulkPlan]
	categoryBulkPlanStore                *batch.Store[categoryBulkPlan]
	supplierPartBulkPlanStore            *batch.Store[supplierPartBulkPlan]
	manufacturerPartBulkPlanStore        *batch.Store[manufacturerPartBulkPlan]
	stockMetadataBulkPlanStore           *batch.Store[stockMetadataBulkPlan]
	stockStatusBulkPlanStore             *batch.Store[stockStatusBulkPlan]
	purchaseOrderBulkPlanStore           *batch.Store[purchaseOrderBulkPlan]
	purchaseOrderLineBulkPlanStore       *batch.Store[purchaseOrderLineBulkPlan]
	purchaseOrderExtraLineBulkPlanStore  *batch.Store[purchaseOrderExtraLineBulkPlan]
}

type attachmentTarget struct {
	modelType string
	modelID   int
}

type loseMutationResponseTransport struct {
	base   http.RoundTripper
	method string
	path   string
	lost   bool
}

func (t *loseMutationResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(req)
	if err != nil || t.lost || req.Method != t.method || req.URL.Path != t.path {
		return response, err
	}
	t.lost = true
	if response != nil {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	return nil, errors.New("injected response loss after live mutation")
}

func newMilestoneToolFixture(t *testing.T, shared *testenv.SharedInvenTree) milestoneToolFixture {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	run, err := shared.NewRun(t)
	r.NoError(err)
	account, err := shared.Account(ctx, run, testenv.AccountAdmin)
	r.NoError(err)
	client, err := shared.Client(account)
	r.NoError(err)

	return milestoneToolFixture{
		shared:                               shared,
		run:                                  run,
		account:                              account,
		client:                               client,
		stockPlanStore:                       newStockPlanStore(time.Now, randomStockPlanToken),
		stockProvenancePlanStore:             newStockProvenancePlanStore(time.Now, randomStockPlanToken),
		parameterPlanStore:                   newParameterPlanStore(time.Now, randomStockPlanToken),
		partFamilyPlanStore:                  newPartFamilyPlanStore(time.Now, randomStockPlanToken),
		partRelationPlanStore:                newPartRelationPlanStore(time.Now, randomStockPlanToken),
		companyRolePlanStore:                 newCompanyRolePlanStore(time.Now, randomStockPlanToken),
		ownerPlanStore:                       newOwnerPlanStore(time.Now, randomStockPlanToken),
		contactPlanStore:                     newContactPlanStore(time.Now, randomStockPlanToken),
		addressPlanStore:                     newAddressPlanStore(time.Now, randomStockPlanToken),
		projectCodePlanStore:                 newProjectCodePlanStore(time.Now, randomStockPlanToken),
		objectParameterDeletePlanStore:       newObjectParameterDeletePlanStore(time.Now, randomStockPlanToken),
		parameterTemplateUniquenessPlanStore: newParameterTemplateUniquenessPlanStore(time.Now, randomStockPlanToken),
		purchaseOrderLifecyclePlanStore:      newPurchaseOrderLifecyclePlanStore(time.Now, randomStockPlanToken),
		partBulkPlanStore: mustBulkStore(batch.Options[partBulkPlan]{
			IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
			SupersedeKey: func(p partBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		}),
		companyBulkPlanStore: mustBulkStore(batch.Options[companyBulkPlan]{
			IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
			SupersedeKey: func(p companyBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		}),
		categoryBulkPlanStore: mustBulkStore(batch.Options[categoryBulkPlan]{
			IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
			SupersedeKey: func(p categoryBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		}),
		supplierPartBulkPlanStore: mustBulkStore(batch.Options[supplierPartBulkPlan]{
			IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
			SupersedeKey: func(p supplierPartBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		}),
		manufacturerPartBulkPlanStore: mustBulkStore(batch.Options[manufacturerPartBulkPlan]{
			IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
			SupersedeKey: func(p manufacturerPartBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		}),
		stockMetadataBulkPlanStore: mustBulkStore(batch.Options[stockMetadataBulkPlan]{
			IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
			SupersedeKey: func(p stockMetadataBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		}),
		stockStatusBulkPlanStore: mustBulkStore(batch.Options[stockStatusBulkPlan]{
			IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
			SupersedeKey: func(p stockStatusBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		}),
		purchaseOrderBulkPlanStore: mustBulkStore(batch.Options[purchaseOrderBulkPlan]{
			IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
			SupersedeKey: func(p purchaseOrderBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		}),
		purchaseOrderLineBulkPlanStore: mustBulkStore(batch.Options[purchaseOrderLineBulkPlan]{
			IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
			SupersedeKey: func(p purchaseOrderLineBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		}),
		purchaseOrderExtraLineBulkPlanStore: mustBulkStore(batch.Options[purchaseOrderExtraLineBulkPlan]{
			IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
			SupersedeKey: func(p purchaseOrderExtraLineBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		}),
	}
}

func (f milestoneToolFixture) deps() Dependencies {
	return Dependencies{
		ClientFromContext: func(context.Context) (any, error) {
			return f.client, nil
		},
		UploadMode:                           upload.ModeStdio,
		UploadMaxBytes:                       upload.DefaultMaxBytes,
		stockPlanStore:                       f.stockPlanStore,
		stockProvenancePlanStore:             f.stockProvenancePlanStore,
		parameterPlanStore:                   f.parameterPlanStore,
		partFamilyPlanStore:                  f.partFamilyPlanStore,
		partRelationPlanStore:                f.partRelationPlanStore,
		companyRolePlanStore:                 f.companyRolePlanStore,
		ownerPlanStore:                       f.ownerPlanStore,
		contactPlanStore:                     f.contactPlanStore,
		addressPlanStore:                     f.addressPlanStore,
		projectCodePlanStore:                 f.projectCodePlanStore,
		objectParameterDeletePlanStore:       f.objectParameterDeletePlanStore,
		parameterTemplateUniquenessPlanStore: f.parameterTemplateUniquenessPlanStore,
		purchaseOrderLifecyclePlanStore:      f.purchaseOrderLifecyclePlanStore,
		partBulkPlanStore:                    f.partBulkPlanStore,
		companyBulkPlanStore:                 f.companyBulkPlanStore,
		categoryBulkPlanStore:                f.categoryBulkPlanStore,
		supplierPartBulkPlanStore:            f.supplierPartBulkPlanStore,
		manufacturerPartBulkPlanStore:        f.manufacturerPartBulkPlanStore,
		stockMetadataBulkPlanStore:           f.stockMetadataBulkPlanStore,
		stockStatusBulkPlanStore:             f.stockStatusBulkPlanStore,
		purchaseOrderBulkPlanStore:           f.purchaseOrderBulkPlanStore,
		purchaseOrderLineBulkPlanStore:       f.purchaseOrderLineBulkPlanStore,
		purchaseOrderExtraLineBulkPlanStore:  f.purchaseOrderExtraLineBulkPlanStore,
	}
}

func (f milestoneToolFixture) ensure(t *testing.T, kind testenv.FixtureKind) testenv.FixtureRecord {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	record, err := f.shared.EnsureFixture(ctx, f.account, f.run, kind)
	r.NoError(err)
	if kind != testenv.FixturePurchaseOrder {
		r.NoError(f.run.RequireOwnedName(record.Name))
	}
	return record
}

func setPurchaseOrderAutoComplete(t *testing.T, ctx context.Context, client *inventree.Client, enabled bool) {
	t.Helper()
	r := require.New(t)
	value := "False"
	if enabled {
		value = "True"
	}
	var setting map[string]any
	r.NoError(client.Patch(ctx, "/api/settings/global/PURCHASEORDER_AUTO_COMPLETE/", inventree.PatchFields{"value": inventree.Set(value)}, &setting))
}

func purchaseOrderAutoCompleteSetting(t *testing.T, ctx context.Context, client *inventree.Client) bool {
	t.Helper()
	r := require.New(t)
	var setting struct {
		Value *bool `json:"value"`
	}
	req, err := client.NewRequest(ctx, http.MethodGet, "/api/settings/global/PURCHASEORDER_AUTO_COMPLETE/", nil, nil)
	r.NoError(err)
	r.NoError(client.DoJSON(req, &setting))
	r.NotNil(setting.Value)
	return *setting.Value
}

func attachmentTargetModelTypes() []string {
	return []string{"part", "stockitem", "company", "supplierpart", "manufacturerpart", "purchaseorder"}
}

func clarificationCandidateIDs(clarification ClarificationResponse) []string {
	ids := make([]string, 0, len(clarification.Candidates))
	for _, candidate := range clarification.Candidates {
		ids = append(ids, candidate.ID)
	}
	return ids
}

func (f milestoneToolFixture) attachmentTarget(t *testing.T, modelType string) attachmentTarget {
	t.Helper()
	part := f.ensure(t, testenv.FixturePart)
	switch modelType {
	case "part":
		return attachmentTarget{modelType: modelType, modelID: part.ID}
	case "stockitem":
		stock := f.createStockItem(t, part.ID, f.ensure(t, testenv.FixtureLocation).ID)
		return attachmentTarget{modelType: modelType, modelID: stock.PK}
	case "company":
		supplier := f.ensure(t, testenv.FixtureSupplier)
		return attachmentTarget{modelType: modelType, modelID: supplier.ID}
	case "supplierpart":
		supplierPart := f.ensure(t, testenv.FixtureSupplierPart)
		return attachmentTarget{modelType: modelType, modelID: supplierPart.ID}
	case "manufacturerpart":
		manufacturer := f.ensure(t, testenv.FixtureManufacturer)
		manufacturerPart := f.createManufacturerPart(t, part.ID, manufacturer.ID)
		return attachmentTarget{modelType: modelType, modelID: manufacturerPart.PK}
	case "purchaseorder":
		purchaseOrder := f.ensure(t, testenv.FixturePurchaseOrder)
		return attachmentTarget{modelType: modelType, modelID: purchaseOrder.ID}
	default:
		require.Failf(t, "unsupported attachment target", "model_type=%s", modelType)
		return attachmentTarget{}
	}
}

func (f milestoneToolFixture) createPart(t *testing.T, suffix string) inventree.Part {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	category := f.ensure(t, testenv.FixtureCategory)
	location := f.ensure(t, testenv.FixtureLocation)
	name, err := f.run.Name(suffix)
	r.NoError(err)
	part, err := f.client.CreatePart(ctx, inventree.PartCreate{
		Name:            name,
		Category:        dvgoutils.Ptr(category.ID),
		DefaultLocation: dvgoutils.Ptr(location.ID),
		Active:          dvgoutils.Ptr(true),
		Component:       dvgoutils.Ptr(true),
		Purchaseable:    dvgoutils.Ptr(true),
		Assembly:        dvgoutils.Ptr(false),
		Trackable:       dvgoutils.Ptr(false),
		Virtual:         dvgoutils.Ptr(false),
	})
	r.NoError(err)
	r.NotZero(part.PK)
	return part
}

func (f milestoneToolFixture) createStockItem(t *testing.T, partID int, locationID int) inventree.StockItem {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	stock, err := f.client.CreateStockItem(ctx, inventree.StockItemCreate{
		Part:     partID,
		Location: locationID,
		Quantity: 3,
	})
	r.NoError(err)
	r.NotZero(stock.PK)
	return stock
}

func (f milestoneToolFixture) createManufacturerPart(t *testing.T, partID int, manufacturerID int) inventree.ManufacturerPart {
	t.Helper()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	mpn, err := f.run.Name("mfgpart")
	r.NoError(err)
	part, err := f.client.CreateManufacturerPart(ctx, inventree.ManufacturerPartCreate{
		Part:         partID,
		Manufacturer: manufacturerID,
		MPN:          dvgoutils.Ptr(mpn),
	})
	r.NoError(err)
	r.NotZero(part.PK)
	return part
}

func findPurchaseOrderLine(lines []inventree.PurchaseOrderLineItem, id int) (inventree.PurchaseOrderLineItem, bool) {
	for _, line := range lines {
		if line.PK == id {
			return line, true
		}
	}
	return inventree.PurchaseOrderLineItem{}, false
}

func allowLocalTestServerFetcher(t *testing.T, rawURL string) upload.URLFetcher {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	return upload.URLFetcher{
		Resolver: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		Allowlist: []upload.URLAllowlistEntry{{
			Scheme: parsed.Scheme,
			Host:   parsed.Hostname(),
			Port:   parsed.Port(),
		}},
	}
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func tinyPNGBytes() []byte {
	return tinyPNGColorBytes(color.NRGBA{R: 255, A: 255})
}

func tinyPNGColorBytes(pixel color.NRGBA) []byte {
	var buf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, pixel)
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func tinyJPEGBytes() []byte {
	var buf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	img.SetNRGBA(0, 0, color.NRGBA{G: 255, A: 255})
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func tinyWebPBytes() []byte {
	content, err := base64.StdEncoding.DecodeString("UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==")
	if err != nil {
		panic(err)
	}
	return content
}

func outputRoleCompany(t *testing.T, ctx context.Context, client *inventree.Client, id int) inventree.CompanyDetail {
	t.Helper()
	company, err := client.GetCompanyDetail(ctx, id)
	require.NoError(t, err)
	return company
}

func currentUserOwnerPK(t *testing.T, ctx context.Context, client *inventree.Client) int {
	t.Helper()
	user, err := client.GetCurrentUser(ctx)
	require.NoError(t, err)
	page, err := client.SearchOwnersPage(ctx, inventree.OwnerQuery{Search: user.Username, Limit: 50})
	require.NoError(t, err)
	require.NotEmpty(t, page.Results, "expected at least one owner match for username %q", user.Username)
	return page.Results[0].PK
}
