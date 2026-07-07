//go:build !no_integration_tests

package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
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
		_, _, err = uploadAttachment(deps)(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
			ModelType:   "part",
			ModelID:     part.ID,
			ContentType: "text/plain",
			LocalPath:   outsidePath,
		})
		r.ErrorContains(err, "outside allowlisted roots")

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
		_, partImage, err := downloadPartImage(fixture.deps())(ctx, &mcp.CallToolRequest{}, DownloadInput{
			ID:       part.ID,
			Mode:     string(inventree.AttachmentContentOriginal),
			MaxBytes: int64(len(imageBytes) + 1),
		})
		r.NoError(err)
		a.Equal(StatusOK, partImage.Status)
		a.Equal(sha256Hex(imageBytes), partImage.SHA256)
		a.Equal(base64.StdEncoding.EncodeToString(imageBytes), partImage.Base64)

		_, _, err = downloadPartImage(fixture.deps())(ctx, &mcp.CallToolRequest{}, DownloadInput{
			ID:       part.ID,
			Mode:     string(inventree.AttachmentContentOriginal),
			MaxBytes: int64(len(imageBytes) - 1),
		})
		r.ErrorContains(err, "exceeds maxBytes")

		_, thumbnail, err := downloadPartImage(fixture.deps())(ctx, &mcp.CallToolRequest{}, DownloadInput{
			ID:       part.ID,
			Mode:     string(inventree.AttachmentContentThumbnail),
			MaxBytes: 4096,
		})
		r.NoError(err)
		a.Equal(StatusOK, thumbnail.Status)
		a.Equal(string(inventree.AttachmentContentThumbnail), thumbnail.Mode)
		a.NotZero(thumbnail.Size)

		noImagePart := fixture.createPart(t, "noimage")
		_, noImage, err := downloadPartImage(fixture.deps())(ctx, &mcp.CallToolRequest{}, DownloadInput{ID: noImagePart.PK})
		r.NoError(err)
		a.Equal(StatusNoImage, noImage.Status)
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
	shared  *testenv.SharedInvenTree
	run     *testenv.Run
	account *testenv.Account
	client  *inventree.Client
}

type attachmentTarget struct {
	modelType string
	modelID   int
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
		shared:  shared,
		run:     run,
		account: account,
		client:  client,
	}
}

func (f milestoneToolFixture) deps() Dependencies {
	return Dependencies{
		ClientFromContext: func(context.Context) (any, error) {
			return f.client, nil
		},
		UploadMode:     upload.ModeStdio,
		UploadMaxBytes: upload.DefaultMaxBytes,
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

func attachmentTargetModelTypes() []string {
	return []string{"part", "stockitem", "company", "supplierpart", "manufacturerpart", "purchaseorder"}
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
	var buf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
