package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadAttachmentUsesInlineBytesAndDuplicatePreflight(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := uploadAttachment(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
		ModelType:    "part",
		ModelID:      10,
		Filename:     "datasheet.txt",
		ContentType:  "text/plain",
		InlineBase64: base64.StdEncoding.EncodeToString([]byte("hello")),
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal("inline", output.SourceKind)
	a.True(fake.uploadedAttachment)
	a.Equal("part", fake.lastAttachmentCreate.ModelType)
	a.Equal(10, fake.lastAttachmentCreate.ModelID)
	a.Equal("datasheet.txt", fake.lastAttachmentCreate.Filename)
	a.Equal("text/plain", fake.lastAttachmentCreate.ContentType)
	a.Equal([]byte("hello"), fake.lastAttachmentCreate.Content)
	a.Equal(inventree.AttachmentQuery{ModelType: "part", ModelID: 10, Limit: MaxLookupLimit}, fake.lastListAttachmentsQuery)

	size := int64(5)
	fake = &fakeMilestoneLookupClient{attachments: []inventree.Attachment{{PK: 90, ModelType: "part", ModelID: 10, Filename: "datasheet.txt", FileSize: &size}}}
	_, output, err = uploadAttachment(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
		ModelType:    "part",
		ModelID:      10,
		Filename:     "datasheet.txt",
		ContentType:  "text/plain",
		InlineBase64: base64.StdEncoding.EncodeToString([]byte("hello")),
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("allow_duplicate", output.Clarification.Retry)
	a.False(fake.uploadedAttachment)
}

func TestUploadAttachmentValidatesInlineAndLocalSources(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeMilestoneLookupClient{}
	_, output, err := uploadAttachment(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
		ModelType:    "part",
		ModelID:      10,
		Filename:     "datasheet.txt",
		InlineBase64: base64.StdEncoding.EncodeToString([]byte("hello")),
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("content_type", output.Clarification.Retry)
	a.False(fake.uploadedAttachment)

	deps := depsForFake(fake)
	deps.UploadMaxBytes = 4
	_, _, err = uploadAttachment(deps)(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
		ModelType:    "part",
		ModelID:      10,
		Filename:     "datasheet.txt",
		ContentType:  "text/plain",
		InlineBase64: base64.StdEncoding.EncodeToString([]byte("hello")),
	})
	r.ErrorContains(err, "exceeds upload max bytes")
	a.False(fake.uploadedAttachment)

	_, output, err = uploadAttachment(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
		ModelType:   "part",
		ModelID:     10,
		Filename:    "datasheet.txt",
		ContentType: "text/plain",
		LocalPath:   "https://example.test/datasheet.pdf",
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("url", output.Clarification.Retry)

	fs := afero.NewMemMapFs()
	r.NoError(afero.WriteFile(fs, "/uploads/datasheet.txt", []byte("local bytes"), 0o644))
	fake = &fakeMilestoneLookupClient{}
	deps = depsForFake(fake)
	deps.UploadMode = upload.ModeStdio
	deps.UploadFS = fs
	deps.UploadAllowRoots = []string{"/uploads"}
	_, output, err = uploadAttachment(deps)(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
		ModelType:   "part",
		ModelID:     10,
		ContentType: "text/plain",
		LocalPath:   "/uploads/datasheet.txt",
	})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal("local_path", output.SourceKind)
	a.Equal("datasheet.txt", fake.lastAttachmentCreate.Filename)
	a.Equal([]byte("local bytes"), fake.lastAttachmentCreate.Content)

	fake = &fakeMilestoneLookupClient{}
	deps.UploadMode = upload.ModeHTTP
	deps.ClientFromContext = func(context.Context) (any, error) { return fake, nil }
	_, _, err = uploadAttachment(deps)(ctx, &mcp.CallToolRequest{}, UploadAttachmentInput{
		ModelType:   "part",
		ModelID:     10,
		ContentType: "text/plain",
		LocalPath:   "/uploads/datasheet.txt",
	})
	r.ErrorContains(err, "HTTP mode rejects local upload paths")
	a.False(fake.uploadedAttachment)
}

func TestUploadAttachmentFromURLFetchesThroughPolicy(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		a.Empty(req.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="datasheet.pdf"`)
		_, _ = w.Write([]byte("pdf bytes"))
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	r.NoError(err)
	fake := &fakeMilestoneLookupClient{}
	deps := depsForFake(fake)
	deps.URLFetcher = upload.URLFetcher{
		Resolver: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		Allowlist: []upload.URLAllowlistEntry{{
			Scheme: parsed.Scheme,
			Host:   parsed.Hostname(),
			Port:   parsed.Port(),
		}},
	}

	_, output, err := uploadAttachmentFromURL(deps)(ctx, &mcp.CallToolRequest{}, UploadAttachmentFromURLInput{
		ModelType: "part",
		ModelID:   10,
		URL:       server.URL + "/datasheet.pdf",
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal("url", output.SourceKind)
	a.True(fake.uploadedAttachment)
	a.Equal("datasheet.pdf", fake.lastAttachmentCreate.Filename)
	a.Equal("application/pdf", fake.lastAttachmentCreate.ContentType)
	a.Equal([]byte("pdf bytes"), fake.lastAttachmentCreate.Content)
}

func TestUploadAttachmentFromURLChecksKnownFilenameDuplicatesBeforeFetch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fetched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetched = true
		_, _ = w.Write([]byte("pdf bytes"))
	}))
	t.Cleanup(server.Close)
	fake := &fakeMilestoneLookupClient{
		attachments: []inventree.Attachment{{PK: 90, ModelType: "part", ModelID: 10, Filename: "datasheet.pdf"}},
	}
	deps := depsForFake(fake)

	_, output, err := uploadAttachmentFromURL(deps)(ctx, &mcp.CallToolRequest{}, UploadAttachmentFromURLInput{
		ModelType: "part",
		ModelID:   10,
		URL:       server.URL,
		Filename:  " /tmp/datasheet.pdf ",
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("allow_duplicate", output.Clarification.Retry)
	a.False(fetched)
	a.False(fake.uploadedAttachment)
}

func TestAttachmentLinkUpdateAndDeleteToolsValidateIntent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		attachment: inventree.Attachment{PK: 90, ModelType: "part", ModelID: 10, Filename: "datasheet"},
	}

	_, output, err := createLinkAttachment(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateLinkAttachmentInput{
		ModelType: "part",
		ModelID:   10,
		URL:       "https://example.test/datasheet.pdf",
		Filename:  "datasheet",
	})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(fake.createdLinkAttachment)
	a.Equal("https://example.test/datasheet.pdf", fake.lastAttachmentCreate.Link)

	duplicateFake := &fakeMilestoneLookupClient{
		attachments: []inventree.Attachment{{PK: 91, ModelType: "part", ModelID: 10, Filename: "datasheet.pdf"}},
	}
	_, output, err = createLinkAttachment(depsForFake(duplicateFake))(ctx, &mcp.CallToolRequest{}, CreateLinkAttachmentInput{
		ModelType: "part",
		ModelID:   10,
		URL:       "https://example.test/other.pdf",
		Filename:  " /tmp/datasheet.pdf ",
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("allow_duplicate", output.Clarification.Retry)
	a.False(duplicateFake.createdLinkAttachment)

	_, _, err = createLinkAttachment(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateLinkAttachmentInput{
		ModelType: "part",
		ModelID:   10,
		URL:       "https://user:pass@example.test/datasheet.pdf",
	})
	r.ErrorContains(err, "must not include userinfo")

	comment := ""
	_, output, err = updateAttachmentMetadata(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpdateAttachmentMetadataInput{ID: 90, Comment: &comment})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(inventree.PatchFields{"comment": inventree.Set("")}, fake.lastUpdateAttachmentFields)

	_, output, err = deleteAttachment(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, DeleteAttachmentInput{ID: 90})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.False(fake.deletedAttachment)

	_, output, err = deleteAttachment(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, DeleteAttachmentInput{ID: 90, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(fake.deletedAttachment)
	a.Equal(90, fake.lastDeleteAttachmentID)
}

func TestSetPrimaryImageRequiresPartImageAttachmentAndConfirmForReplacement(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	imageURL := "/media/part_images/resistor.png"
	existingURL := "/media/part_images/old.png"
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Name: "resistor", Image: &existingURL},
		attachment: inventree.Attachment{
			PK:         90,
			ModelType:  "part",
			ModelID:    10,
			Filename:   "resistor.png",
			Attachment: &imageURL,
			IsImage:    true,
		},
		downloadedAttachment: inventree.DownloadedAttachment{
			Attachment:  inventree.Attachment{PK: 90, Filename: "resistor.png"},
			Content:     []byte("png bytes"),
			ContentType: "image/png",
		},
	}

	_, output, err := setPrimaryImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPrimaryImageInput{PartID: 10, AttachmentID: 90})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("confirm", output.Clarification.Retry)
	a.False(fake.setPartPrimaryImage)

	_, output, err = setPrimaryImage(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPrimaryImageInput{PartID: 10, AttachmentID: 90, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(10, output.PartID)
	a.True(output.Replaced)
	a.True(fake.setPartPrimaryImage)
	a.Equal(upload.DefaultMaxBytes, fake.lastAttachmentMaxBytes)
	a.Equal(10, fake.lastSetPartPrimaryImagePartID)
	a.Equal(inventree.PartPrimaryImageCreate{Filename: "resistor.png", ContentType: "image/png", Content: []byte("png bytes")}, fake.lastSetPartPrimaryImageInput)
	a.Equal("/media/part_images/resistor.png", output.ImageURL)

	wrongPart := &fakeMilestoneLookupClient{
		part:       inventree.Part{PK: 10, Name: "resistor"},
		attachment: inventree.Attachment{PK: 91, ModelType: "part", ModelID: 11, Filename: "other.png", Attachment: &imageURL, IsImage: true},
	}
	_, output, err = setPrimaryImage(depsForFake(wrongPart))(ctx, &mcp.CallToolRequest{}, SetPrimaryImageInput{PartID: 10, AttachmentID: 91})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("attachment_id", output.Clarification.Retry)
	a.False(wrongPart.setPartPrimaryImage)

	notImage := &fakeMilestoneLookupClient{
		part:       inventree.Part{PK: 10, Name: "resistor"},
		attachment: inventree.Attachment{PK: 92, ModelType: "part", ModelID: 10, Filename: "datasheet.pdf", Attachment: &imageURL},
	}
	_, output, err = setPrimaryImage(depsForFake(notImage))(ctx, &mcp.CallToolRequest{}, SetPrimaryImageInput{PartID: 10, AttachmentID: 92})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("attachment_id", output.Clarification.Retry)
	a.False(notImage.setPartPrimaryImage)

	limited := &fakeMilestoneLookupClient{
		part:                 inventree.Part{PK: 10, Name: "resistor"},
		attachment:           inventree.Attachment{PK: 93, ModelType: "part", ModelID: 10, Filename: "small.png", Attachment: &imageURL, IsImage: true},
		downloadedAttachment: inventree.DownloadedAttachment{Content: []byte("png bytes"), ContentType: "image/png"},
	}
	deps := depsForFake(limited)
	deps.UploadMaxBytes = 123
	_, output, err = setPrimaryImage(deps)(ctx, &mcp.CallToolRequest{}, SetPrimaryImageInput{PartID: 10, AttachmentID: 93})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.False(output.Replaced)
	a.Equal(int64(123), limited.lastAttachmentMaxBytes)
}

func TestDeleteAttachmentMissingConfirmReturnsStructuredClarificationThroughMCP(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	size := int64(34)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "v0.0.0"}, nil)
		Register(server, Dependencies{
			EnableWriteTools: true,
			ClientFromContext: func(context.Context) (any, error) {
				return &fakeMilestoneLookupClient{
					attachment: inventree.Attachment{
						PK:        90,
						ModelType: "part",
						ModelID:   10,
						Filename:  "datasheet.txt",
						FileSize:  &size,
					},
				}, nil
			},
		})
		serverDone <- server.Run(ctx, serverTransport)
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
		Arguments: map[string]any{"id": 90},
	})
	r.NoError(err)
	if result.IsError {
		errorText := ""
		if len(result.Content) > 0 {
			if text, ok := result.Content[0].(*mcp.TextContent); ok {
				errorText = text.Text
			}
		}
		t.Fatalf("delete_attachment returned tool error: %s", errorText)
	}
	a.False(result.IsError)
	structured := result.StructuredContent.(map[string]any)
	a.Equal(StatusClarificationRequired, structured["status"])
	record := structured["record"].(map[string]any)
	a.Equal(float64(90), record["pk"])
	clarification := structured["clarification"].(map[string]any)
	a.Equal(StatusClarificationRequired, clarification["status"])
	a.Equal("confirm", clarification["retry"])
	candidates := clarification["candidates"].([]any)
	r.Len(candidates, 1)
	fields := candidates[0].(map[string]any)["fields"].(map[string]any)
	a.Equal(float64(34), fields["file_size"])
}

func TestWriteToolAuthorizationsUseWriteScope(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	for _, name := range writeToolNames {
		auth, ok := ToolAuthorizations[name]
		r.True(ok, "missing authorization for %s", name)
		switch name {
		case CreateParameterTemplateToolName, UpdateParameterTemplateToolName, CreateCategoryParameterDefaultToolName, UpdateCategoryParameterDefaultToolName, CreatePartCategoryToolName, CreateStockLocationToolName, CreatePurchaseOrderExtraLineToolName, CreatePurchaseOrderWorkflowToolName, IssuePurchaseOrderToolName:
			a.Equal("write", auth.MutationClass)
			a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite}, auth.Scopes)
			a.Equal(WriteAnnotations, auth.Annotations)
		case UpdatePartCategoryToolName:
			a.Equal("write", auth.MutationClass)
			a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite}, auth.Scopes)
			expected := WriteAnnotations
			expected.Idempotent = true
			a.Equal(expected, auth.Annotations)
		case UpdateCompanyToolName, UpdateSupplierPartToolName, UpdateManufacturerPartToolName, UpdateStockLocationToolName, UpdatePurchaseOrderExtraLineToolName:
			a.Equal("write", auth.MutationClass)
			a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite}, auth.Scopes)
			expected := WriteAnnotations
			expected.Idempotent = true
			a.Equal(expected, auth.Annotations)
		case BulkPropagatePartParametersToolName:
			a.Equal("destructive", auth.MutationClass)
			a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeDestructive}, auth.Scopes)
			a.True(auth.Annotations.Destructive)
		case DepleteStockItemToolName:
			a.Equal("destructive", auth.MutationClass)
			a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational, ScopeInventreeDestructive}, auth.Scopes)
			a.True(auth.Annotations.Destructive)
		case CreateStockItemToolName, InitialStockWorkflowToolName:
			a.Equal("operational", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite, ScopeInventreeOperational}, auth.Scopes)
		case AdjustStockQuantityToolName, SetStockStatusToolName, StocktakeAdjustmentToolName, ReceivePurchaseOrderToolName, RestructureStockLocationToolName, UpdateStockItemMetadataToolName:
			a.Equal("operational", auth.MutationClass)
			a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational}, auth.Scopes)
			if name == RestructureStockLocationToolName || name == UpdateStockItemMetadataToolName {
				expected := WriteAnnotations
				expected.Idempotent = true
				a.Equal(expected, auth.Annotations)
			}
		case UploadAttachmentToolName, CreateLinkAttachmentToolName, UpdateAttachmentMetadataToolName, SetPrimaryImageToolName:
			a.Equal("write", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite, ScopeInventreeUpload}, auth.Scopes)
		case UploadAttachmentFromURLToolName:
			a.Equal("write", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite, ScopeInventreeUpload}, auth.Scopes)
			a.True(auth.Annotations.OpenWorld)
		case DeleteAttachmentToolName:
			a.Equal("destructive", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite, ScopeInventreeUpload, ScopeInventreeDestructive}, auth.Scopes)
			a.True(auth.Annotations.Destructive)
		case DeletePartParameterToolName, DeleteParameterTemplateToolName, MergeParameterTemplatesToolName, DeleteCategoryParameterDefaultToolName, DeletePurchaseOrderExtraLineToolName:
			a.Equal("destructive", auth.MutationClass)
			a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeDestructive}, auth.Scopes)
			a.True(auth.Annotations.Destructive)
		default:
			a.Equal("write", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite}, auth.Scopes)
			a.Equal(WriteAnnotations, auth.Annotations)
		}
	}
}

func TestWriteToolInputsExcludeSalesAndCustomerWorkflowFields(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	for _, schemaType := range []reflect.Type{
		reflect.TypeOf(CreatePartInput{}),
		reflect.TypeOf(UpdatePartInput{}),
		reflect.TypeOf(CreateCompanyInput{}),
		reflect.TypeOf(CreateSupplierPartInput{}),
		reflect.TypeOf(CreateManufacturerPartInput{}),
		reflect.TypeOf(UpsertPartWorkflowInput{}),
		reflect.TypeOf(InitialStockWorkflowInput{}),
		reflect.TypeOf(CreateStockItemInput{}),
		reflect.TypeOf(AdjustStockQuantityInput{}),
		reflect.TypeOf(SetStockStatusInput{}),
		reflect.TypeOf(StocktakeAdjustmentInput{}),
		reflect.TypeOf(DepleteStockItemInput{}),
		reflect.TypeOf(IssuePurchaseOrderInput{}),
		reflect.TypeOf(ReceivePurchaseOrderInput{}),
		reflect.TypeOf(ReceivePurchaseOrderItem{}),
		reflect.TypeOf(CreatePurchaseOrderExtraLineInput{}),
		reflect.TypeOf(UpdatePurchaseOrderExtraLineInput{}),
		reflect.TypeOf(DeletePurchaseOrderExtraLineInput{}),
		reflect.TypeOf(PurchaseOrderWorkflowExtraLine{}),
		reflect.TypeOf(SetPartParametersInput{}),
		reflect.TypeOf(ParameterSetInput{}),
		reflect.TypeOf(UploadAttachmentInput{}),
		reflect.TypeOf(UploadAttachmentFromURLInput{}),
		reflect.TypeOf(CreateLinkAttachmentInput{}),
		reflect.TypeOf(UpdateAttachmentMetadataInput{}),
		reflect.TypeOf(DeleteAttachmentInput{}),
		reflect.TypeOf(SetPrimaryImageInput{}),
		reflect.TypeOf(inventree.PartCreate{}),
		reflect.TypeOf(inventree.CompanyCreate{}),
		reflect.TypeOf(inventree.SupplierPartCreate{}),
		reflect.TypeOf(inventree.ManufacturerPartCreate{}),
		reflect.TypeOf(inventree.StockItemCreate{}),
		reflect.TypeOf(inventree.PurchaseOrderReceive{}),
		reflect.TypeOf(inventree.PurchaseOrderReceiveItem{}),
		reflect.TypeOf(inventree.ParameterCreate{}),
		reflect.TypeOf(inventree.AttachmentCreate{}),
	} {
		for _, field := range reflect.VisibleFields(schemaType) {
			jsonName := jsonFieldName(field.Tag.Get("json"))
			a.NotContains(strings.ToLower(field.Name), "customer")
			a.NotContains(strings.ToLower(jsonName), "customer")
			a.NotContains(strings.ToLower(field.Name), "salable")
			a.NotContains(strings.ToLower(jsonName), "salable")
			a.NotContains(strings.ToLower(field.Name), "sales")
			a.NotContains(strings.ToLower(jsonName), "sales")
		}
	}
}

func TestUpsertPartWorkflowDryRunPlansWithoutWrites(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}
	purchaseable := true

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		DryRun:               true,
		Name:                 "10k resistor",
		CategoryID:           20,
		Purchaseable:         &purchaseable,
		SupplierName:         "Acme",
		SupplierCurrency:     "AUD",
		SupplierSKU:          "ACME-10K",
		ManufacturerName:     "PartsCo",
		ManufacturerCurrency: "AUD",
		MPN:                  dvgoutils.Ptr("RC0603-10K"),
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.DryRun)
	a.Equal([]PartUpsertWorkflowAction{
		{Name: "create_part", Status: "planned", RecordType: "part", Reason: "no matching part found"},
		{Name: "create_manufacturer", Status: "planned", RecordType: "company", Reason: "no matching manufacturer found"},
		{Name: "create_manufacturer_part", Status: "planned", RecordType: "manufacturerpart", Reason: "new part or manufacturer would be created first"},
		{Name: "create_supplier", Status: "planned", RecordType: "company", Reason: "no matching supplier found"},
		{Name: "create_supplier_part", Status: "planned", RecordType: "supplierpart", Reason: "new part or supplier would be created first"},
	}, output.Actions)
	a.Nil(output.Part)
	a.Nil(output.Supplier)
	a.Nil(output.Manufacturer)
	a.Equal([]PlannedChange{
		{Action: "create_part", RecordType: "part", Fields: map[string]any{"name": "10k resistor", "category": 20, "purchaseable": true}},
		{Action: "create_manufacturer", RecordType: "company", Fields: map[string]any{"name": "PartsCo", "currency": "AUD", "is_manufacturer": true}},
		{Action: "create_manufacturer_part", RecordType: "manufacturerpart", Fields: map[string]any{"MPN": "RC0603-10K"}, DependsOn: []PlannedChangeDependency{{Field: "part", Action: "create_part"}, {Field: "manufacturer", Action: "create_manufacturer"}}},
		{Action: "create_supplier", RecordType: "company", Fields: map[string]any{"name": "Acme", "currency": "AUD", "is_supplier": true}},
		{Action: "create_supplier_part", RecordType: "supplierpart", Fields: map[string]any{"SKU": "ACME-10K"}, DependsOn: []PlannedChangeDependency{{Field: "part", Action: "create_part"}, {Field: "supplier", Action: "create_supplier"}, {Field: "manufacturer_part", Action: "create_manufacturer_part"}}},
	}, output.PlannedChanges)
	a.False(fake.createdPart)
	a.False(fake.createdCompany)
	a.False(fake.createdManufacturerPart)
	a.False(fake.createdSupplierPart)
	a.Contains(output.OmittedRecommendedFields, "ipn")
	a.Contains(output.OmittedRecommendedFields, "units")
	a.Contains(output.OmittedRecommendedFields, "default_location_id")
	a.NotContains(output.OmittedRecommendedFields, "purchaseable")
}

func TestUpsertPartWorkflowDryRunExposesIssue88EffectiveFields(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{companies: []inventree.Company{{PK: 3, Name: "CoreElectronics", Currency: "AUD", Active: true, IsSupplier: true}}}
	description := "3-pin keyed male PCB header for standard PC motherboard fan connections; 2.54 mm pitch."
	units := "pcs"
	purchaseable := true
	link := "https://core-electronics.com.au/3-pin-male-polarized-header.html"

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		DryRun:       true,
		Name:         "3-Pin PC Fan Header, Male, 2.54 mm",
		CategoryID:   12,
		Description:  &description,
		Purchaseable: &purchaseable,
		SupplierID:   3,
		SupplierSKU:  "CE05304",
		Link:         &link,
		Units:        &units,
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Nil(output.Part)
	r.NotNil(output.Supplier)
	a.Equal(3, output.Supplier.PK)
	a.Equal("CoreElectronics", output.Supplier.Name)
	a.True(output.Supplier.IsSupplier)
	a.Equal(3, fake.lastGetCompanyID)
	a.Equal([]PlannedChange{
		{Action: "create_part", RecordType: "part", Fields: map[string]any{
			"name": "3-Pin PC Fan Header, Male, 2.54 mm", "category": 12, "description": description, "purchaseable": true, "units": "pcs",
		}},
		{Action: "create_supplier_part", RecordType: "supplierpart", Fields: map[string]any{
			"supplier": 3, "SKU": "CE05304", "link": link,
		}, DependsOn: []PlannedChangeDependency{{Field: "part", Action: "create_part"}}},
	}, output.PlannedChanges)
	a.NotContains(output.OmittedRecommendedFields, "units")
	a.NotContains(output.OmittedRecommendedFields, "purchaseable")
	a.False(fake.createdPart)
	a.False(fake.createdSupplierPart)
}

func TestUpsertPartWorkflowDryRunRejectsDirectCompanyWithoutRequestedRole(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{companies: []inventree.Company{{PK: 3, Name: "Customer only", IsSupplier: false}}}

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		DryRun: true, Name: "part", CategoryID: 12, SupplierID: 3, SupplierSKU: "SKU-1",
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("supplier_id", output.Clarification.Retry)
	a.Contains(output.Clarification.Reason, "does not have the supplier role")
	a.False(fake.createdPart)
	a.False(fake.createdSupplierPart)
}

func TestUpsertPartWorkflowDryRunFailsClosedForMissingDirectCompany(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{getCompanyErr: errors.New("company not found")}

	_, _, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		DryRun: true, Name: "part", CategoryID: 12, SupplierID: 3, SupplierSKU: "SKU-1",
	})

	r.Error(err)
	r.False(fake.createdPart)
	r.False(fake.createdSupplierPart)
}

func TestUpsertPartWorkflowDryRunExposesExplicitPartPatchValues(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{part: inventree.Part{PK: 10, Name: "10k resistor", Purchaseable: true}}
	description := ""
	purchaseable := false

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		DryRun: true, PartID: 10, Description: &description, Purchaseable: &purchaseable,
	})

	r.NoError(err)
	a.Equal([]PlannedChange{{
		Action: "update_part", RecordType: "part", ID: 10,
		Fields: map[string]any{"description": "", "purchaseable": false},
	}}, output.PlannedChanges)
	r.NotNil(output.Part)
	a.True(output.Part.Purchaseable)
	a.Empty(fake.lastUpdatePartFields)
}

func TestUpsertPartWorkflowReusesExistingRecords(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		parts:             []inventree.Part{{PK: 10, Name: "10k resistor"}},
		suppliers:         []inventree.Company{{PK: 30, Name: "Acme", IsSupplier: true}},
		manufacturers:     []inventree.Company{{PK: 31, Name: "PartsCo", IsManufacturer: true}},
		supplierParts:     []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "ACME-10K"}},
		manufacturerParts: []inventree.ManufacturerPart{{PK: 50, Part: 10, Manufacturer: 31, MPN: "RC0603-10K"}},
	}

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		Name:             "10k resistor",
		SupplierName:     "Acme",
		SupplierSKU:      "ACME-10K",
		ManufacturerName: "PartsCo",
		MPN:              dvgoutils.Ptr("RC0603-10K"),
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	r.NotNil(output.Part)
	r.NotNil(output.Supplier)
	r.NotNil(output.Manufacturer)
	r.NotNil(output.SupplierPart)
	r.NotNil(output.ManufacturerPart)
	a.Equal(10, output.Part.PK)
	a.Equal(40, output.SupplierPart.PK)
	a.Equal(50, output.ManufacturerPart.PK)
	a.Equal(inventree.SearchQuery{Search: "10k resistor", Limit: DefaultLookupLimit}, fake.lastSearchPartsQuery)
	a.Equal(inventree.SearchQuery{Search: "Acme", Limit: DefaultLookupLimit}, fake.lastSearchSuppliersQuery)
	a.Equal(inventree.SearchQuery{Search: "PartsCo", Limit: DefaultLookupLimit}, fake.lastSearchManufacturersQuery)
	a.Equal(inventree.SupplierPartQuery{Part: 10, Supplier: 30, SKU: "ACME-10K"}, fake.lastSearchSupplierPartsQuery)
	a.Equal(inventree.ManufacturerPartQuery{Part: 10, Manufacturer: 31, MPN: "RC0603-10K"}, fake.lastSearchManufacturerPartsQuery)
	a.False(fake.createdPart)
	a.False(fake.createdCompany)
	a.False(fake.createdSupplierPart)
	a.False(fake.createdManufacturerPart)
}

func TestUpsertPartWorkflowUpdatesSingleNameMatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		parts: []inventree.Part{{PK: 10, Name: "10k resistor"}},
	}
	units := "pcs"
	purchaseable := true

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		Name:         "10k resistor",
		Units:        &units,
		Purchaseable: &purchaseable,
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(inventree.PatchFields{"units": inventree.Set("pcs"), "purchaseable": inventree.Set(true)}, fake.lastUpdatePartFields)
	a.False(fake.createdPart)
}

func TestUpsertPartWorkflowCreatesUnambiguousMissingRecords(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}
	units := "pcs"
	purchaseable := true

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		Name:                 "10k resistor",
		CategoryID:           20,
		Units:                &units,
		Purchaseable:         &purchaseable,
		SupplierName:         "Acme",
		SupplierCurrency:     "AUD",
		SupplierSKU:          "ACME-10K",
		ManufacturerName:     "PartsCo",
		ManufacturerCurrency: "AUD",
		MPN:                  dvgoutils.Ptr("RC0603-10K"),
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(fake.createdPart)
	a.True(fake.createdCompany)
	a.True(fake.createdManufacturerPart)
	a.True(fake.createdSupplierPart)
	a.Equal(inventree.PartCreate{Name: "10k resistor", Category: dvgoutils.Ptr(20), Units: &units, Purchaseable: &purchaseable}, fake.lastCreatePart)
	a.Equal(inventree.CompanyCreate{Name: "Acme", Currency: "AUD", IsSupplier: true}, fake.lastCreateCompany)
	a.Equal(inventree.ManufacturerPartCreate{Part: 10, Manufacturer: 30, MPN: dvgoutils.Ptr("RC0603-10K")}, fake.lastCreateManufacturerPart)
	a.Equal(inventree.SupplierPartCreate{Part: 10, Supplier: 30, SKU: "ACME-10K", ManufacturerPart: dvgoutils.Ptr(50)}, fake.lastCreateSupplierPart)
}

func TestUpsertPartWorkflowSkipsManufacturerPartWithoutMPN(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		parts:         []inventree.Part{{PK: 10, Name: "10k resistor"}},
		suppliers:     []inventree.Company{{PK: 30, Name: "Acme", IsSupplier: true}},
		manufacturers: []inventree.Company{{PK: 31, Name: "PartsCo", IsManufacturer: true}},
	}

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		Name:             "10k resistor",
		SupplierName:     "Acme",
		SupplierSKU:      "ACME-10K",
		ManufacturerName: "PartsCo",
		MPN:              dvgoutils.Ptr(" \t "),
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Nil(output.ManufacturerPart)
	a.False(fake.createdManufacturerPart)
	a.Equal(inventree.ManufacturerPartQuery{Part: 10, Manufacturer: 31}, fake.lastSearchManufacturerPartsQuery)
	a.Equal(inventree.SupplierPartCreate{Part: 10, Supplier: 30, SKU: "ACME-10K"}, fake.lastCreateSupplierPart)
	a.Contains(output.Actions, PartUpsertWorkflowAction{
		Name:       "skip_manufacturer_part",
		Status:     "skipped",
		RecordType: "manufacturerpart",
		Reason:     "MPN not supplied and no existing link matched; no fallback value was invented",
	})
}

func TestRemainingPartUpsertActionsDoNotScheduleManufacturerPartWithoutMPN(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	input := UpsertPartWorkflowInput{
		ManufacturerName: "PartsCo",
		SupplierName:     "Acme",
	}

	a.Equal([]string{"resolve_manufacturer", "resolve_or_skip_manufacturer_part", "resolve_supplier", "create_or_reuse_supplier_part"}, remainingPartUpsertActions(input, "create_part"))
	a.Equal([]string{"resolve_or_skip_manufacturer_part", "resolve_supplier", "create_or_reuse_supplier_part"}, remainingPartUpsertActions(input, "create_manufacturer"))
}

func TestUpsertPartWorkflowReusesExistingManufacturerPartWithoutMPN(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		parts:             []inventree.Part{{PK: 10, Name: "10k resistor"}},
		suppliers:         []inventree.Company{{PK: 30, Name: "Acme", IsSupplier: true}},
		manufacturers:     []inventree.Company{{PK: 31, Name: "PartsCo", IsManufacturer: true}},
		manufacturerParts: []inventree.ManufacturerPart{{PK: 50, Part: 10, Manufacturer: 31, MPN: "known-upstream"}},
	}

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		Name:             "10k resistor",
		SupplierName:     "Acme",
		SupplierSKU:      "ACME-10K",
		ManufacturerName: "PartsCo",
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	r.NotNil(output.ManufacturerPart)
	a.Equal(50, output.ManufacturerPart.PK)
	a.Equal(dvgoutils.Ptr(50), fake.lastCreateSupplierPart.ManufacturerPart)
	a.False(fake.createdManufacturerPart)
}

func TestUpsertPartWorkflowClarifiesMultipleManufacturerPartsWithoutMPNBeforeWriting(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		parts:         []inventree.Part{{PK: 10, Name: "10k resistor"}},
		manufacturers: []inventree.Company{{PK: 31, Name: "PartsCo", IsManufacturer: true}},
		manufacturerParts: []inventree.ManufacturerPart{
			{PK: 50, Part: 10, Manufacturer: 31, MPN: "MPN-1"},
			{PK: 51, Part: 10, Manufacturer: 31, MPN: "MPN-2"},
		},
	}

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		Name:             "10k resistor",
		ManufacturerName: "PartsCo",
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("manufacturer_part_id", output.Clarification.Retry)
	a.False(fake.createdPart)
	a.False(fake.createdManufacturerPart)
	a.False(fake.createdSupplierPart)
}

func TestRunPartUpsertWorkflowPreservesCreatedPartWhenManufacturerLookupFails(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{searchManufacturersErr: &inventree.APIError{StatusCode: http.StatusInternalServerError, Kind: inventree.ErrorKindServer}}

	_, output, err := runPartUpsertWorkflow(ctx, fake, UpsertPartWorkflowInput{
		Name:                 "live order resistor",
		CategoryID:           20,
		ManufacturerName:     "PartsCo",
		ManufacturerCurrency: "AUD",
		SupplierName:         "Acme",
		SupplierCurrency:     "AUD",
		SupplierSKU:          "ORDER-SKU-1",
	})

	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.Part)
	a.Equal(10, output.Part.PK)
	r.NotNil(output.Failure)
	a.Equal("resolve_manufacturer", output.Failure.Action)
	a.NotEmpty(output.Failure.RecoveryPlan)
	a.Contains(output.Actions, PartUpsertWorkflowAction{Name: "create_part", Status: "created", RecordType: "part", ID: 10, Reason: "no matching part found"})
	a.Contains(output.Actions, PartUpsertWorkflowAction{Name: "resolve_manufacturer", Status: "failed", RecordType: "company", Reason: "InvenTree lookup did not complete after an earlier write"})
	a.Equal([]string{"resolve_or_skip_manufacturer_part", "resolve_supplier", "create_or_reuse_supplier_part"}, output.RemainingActions)
}

func TestRunPartUpsertWorkflowPreservesCreatedPartWhenManufacturerLookupBecomesAmbiguous(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{manufacturers: []inventree.Company{
		{PK: 31, Name: "PartsCo", IsManufacturer: true},
		{PK: 32, Name: "PartsCo duplicate", IsManufacturer: true},
	}}

	_, output, err := runPartUpsertWorkflow(ctx, fake, UpsertPartWorkflowInput{
		Name:             "live order resistor",
		CategoryID:       20,
		ManufacturerName: "PartsCo",
	})

	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.Part)
	a.Equal(10, output.Part.PK)
	r.NotNil(output.Failure)
	a.Equal("resolve_manufacturer", output.Failure.Action)
	a.NotEmpty(output.Failure.RecoveryPlan)
	r.NotNil(output.Clarification)
	a.Equal("manufacturer_id", output.Clarification.Retry)
	a.Contains(output.Actions, PartUpsertWorkflowAction{Name: "create_part", Status: "created", RecordType: "part", ID: 10, Reason: "no matching part found"})
	a.Contains(output.Actions, PartUpsertWorkflowAction{Name: "resolve_manufacturer", Status: "failed", RecordType: "company", Reason: "lookup became ambiguous after an earlier write"})
}

func TestUpsertPartWorkflowPreservesCreatedPartWhenManufacturerDisappearsAfterPreflight(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{manufacturerSearchResults: [][]inventree.Company{
		{{PK: 31, Name: "PartsCo", IsManufacturer: true}},
		{},
	}}

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		Name:             "live order resistor",
		CategoryID:       20,
		ManufacturerName: "PartsCo",
	})

	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.Part)
	a.Equal(10, output.Part.PK)
	r.NotNil(output.Failure)
	a.Equal("resolve_manufacturer", output.Failure.Action)
	a.Equal("Provide the company currency, inspect the accumulated records, and continue only the remaining actions.", output.Failure.RecoveryPlan)
	r.NotNil(output.Clarification)
	a.Equal("manufacturer_currency", output.Clarification.Retry)
	a.Contains(output.Actions, PartUpsertWorkflowAction{Name: "create_part", Status: "created", RecordType: "part", ID: 10, Reason: "no matching part found"})
	a.Contains(output.Actions, PartUpsertWorkflowAction{Name: "resolve_manufacturer", Status: "failed", RecordType: "company", Reason: "company no longer matched after preflight"})
}

func TestUpsertPartWorkflowAsksForAmbiguousPart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		parts: []inventree.Part{{PK: 10, Name: "10k resistor"}, {PK: 11, Name: "10k resistor precision"}},
	}

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{Name: "10k resistor", CategoryID: 20})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("part", output.Clarification.Field)
	a.Equal("part_id", output.Clarification.Retry)
	a.Len(output.Clarification.Candidates, 2)
	a.False(fake.createdPart)
}

func TestUpsertPartWorkflowAsksForMissingCreateInputs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{Name: "10k resistor"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("category_id", output.Clarification.Field)
	a.False(fake.createdPart)

	_, output, err = upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{Name: "10k resistor", CategoryID: 20, SupplierName: "Acme"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("supplier_currency", output.Clarification.Field)
	a.False(fake.createdCompany)
}

func TestUpsertPartWorkflowPreflightsClarificationsBeforeWriting(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		Name:                 "10k resistor",
		CategoryID:           20,
		SupplierName:         "Acme",
		SupplierCurrency:     "AUD",
		ManufacturerName:     "PartsCo",
		ManufacturerCurrency: "AUD",
		MPN:                  dvgoutils.Ptr("RC0603-10K"),
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("supplier_sku", output.Clarification.Field)
	a.False(output.DryRun)
	a.False(fake.createdPart)
	a.False(fake.createdCompany)
	a.False(fake.createdManufacturerPart)
	a.False(fake.createdSupplierPart)
}

func TestUpsertPartWorkflowAsksForInvalidExplicitIDs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{PartID: -1, Name: "10k resistor", CategoryID: 20})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("part", output.Clarification.Field)
	a.False(fake.createdPart)

	_, output, err = upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{Name: "10k resistor", CategoryID: 20, SupplierID: -1, SupplierName: "Acme", SupplierCurrency: "AUD", SupplierSKU: "ACME-10K"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("supplier", output.Clarification.Field)
	a.False(fake.createdCompany)

	_, output, err = upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{Name: "10k resistor", CategoryID: 20, ManufacturerID: -1, ManufacturerName: "PartsCo", ManufacturerCurrency: "AUD"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("manufacturer", output.Clarification.Field)
	a.False(fake.createdCompany)
}

func TestCreateStockItemAsksBeforeDuplicateCreate(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	fake := &fakeMilestoneLookupClient{
		stockItems: []inventree.StockItem{{PK: 50, Part: 10, Location: &locationID, Quantity: 2}},
	}

	_, output, err := createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{PartID: 10, LocationID: locationID, Quantity: 7})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("stock_item", output.Clarification.Field)
	a.Equal("stock_item_id", output.Clarification.Retry)
	a.Equal("50", output.Clarification.Candidates[0].ID)
	a.Equal(inventree.StockItemQuery{PartID: 10, LocationID: locationID, Limit: DefaultLookupLimit}, fake.lastSearchStockItemsQuery)
	a.False(fake.createdStockItem)
}

func TestCreateStockItemValidatesInputsBeforeWrite(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{LocationID: 40, Quantity: 1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("part", output.Clarification.Field)

	_, output, err = createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{PartID: 10, Quantity: 1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("location", output.Clarification.Field)

	_, output, err = createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{PartID: 10, LocationID: 40})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("quantity", output.Clarification.Field)

	_, output, err = createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{PartID: 10, LocationID: 40, Quantity: 1, Status: dvgoutils.Ptr(-1)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("status", output.Clarification.Field)

	a.False(fake.createdStockItem)
	a.Equal(inventree.StockItemQuery{}, fake.lastSearchStockItemsQuery)
}

func TestCreateStockItemWritesAfterDuplicatePreflight(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := createStockItem(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateStockItemInput{
		PartID:     10,
		LocationID: 40,
		Quantity:   7,
		Status:     dvgoutils.Ptr(10),
		Batch:      dvgoutils.Ptr("B-1"),
		Notes:      dvgoutils.Ptr("initial stock"),
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(fake.createdStockItem)
	a.Equal(inventree.StockItemQuery{PartID: 10, LocationID: 40, Limit: DefaultLookupLimit}, fake.lastSearchStockItemsQuery)
	a.Equal(inventree.StockItemCreate{Part: 10, Location: 40, Quantity: 7, Status: dvgoutils.Ptr(10), Batch: dvgoutils.Ptr("B-1"), Notes: dvgoutils.Ptr("initial stock")}, fake.lastCreateStockItem)
}

func TestInitialStockWorkflowDryRunPlansWithoutWrite(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		parts:          []inventree.Part{{PK: 10, Name: "10k resistor"}},
		stockLocations: []inventree.StockLocation{{PK: 40, Name: "bin 1"}},
	}

	_, output, err := initialStockWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, InitialStockWorkflowInput{
		DryRun:         true,
		PartSearch:     "10k",
		LocationSearch: "bin",
		Quantity:       7,
		Status:         dvgoutils.Ptr(10),
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.DryRun)
	r.NotNil(output.Part)
	r.NotNil(output.Location)
	a.Equal(10, output.Part.PK)
	a.Equal(40, output.Location.PK)
	a.Equal([]InitialStockWorkflowAction{
		{Name: "reuse_part", Status: "reused", RecordType: "part", ID: 10, Reason: "single matching part found"},
		{Name: "reuse_location", Status: "reused", RecordType: "stocklocation", ID: 40, Reason: "single matching stock location found"},
		{Name: "create_stock_item", Status: "planned", RecordType: "stockitem", Reason: "no matching stock item found"},
	}, output.Actions)
	a.Equal([]PlannedChange{{
		Action: "create_stock_item", RecordType: "stockitem",
		Fields: map[string]any{"part": 10, "location": 40, "quantity": float64(7), "status": 10},
	}}, output.PlannedChanges)
	a.Equal(inventree.SearchQuery{Search: "10k", Limit: DefaultLookupLimit}, fake.lastSearchPartsQuery)
	a.Equal(inventree.SearchQuery{Search: "bin", Limit: DefaultLookupLimit}, fake.lastSearchStockLocationsQuery)
	a.Equal(inventree.StockItemQuery{PartID: 10, LocationID: 40, Limit: DefaultLookupLimit}, fake.lastSearchStockItemsQuery)
	a.False(fake.createdStockItem)
}

func TestInitialStockWorkflowWritesAfterDuplicatePreflight(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		part:           inventree.Part{PK: 10, Name: "10k resistor"},
		stockLocations: []inventree.StockLocation{{PK: 40, Name: "bin 1"}},
	}

	_, output, err := initialStockWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, InitialStockWorkflowInput{
		PartID:     10,
		LocationID: 40,
		Quantity:   7,
		Batch:      dvgoutils.Ptr("B-1"),
		Notes:      dvgoutils.Ptr("initial stock"),
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.False(output.DryRun)
	r.NotNil(output.Part)
	r.NotNil(output.Location)
	a.Equal("10k resistor", output.Part.Name)
	a.Equal("bin 1", output.Location.Name)
	r.NotNil(output.StockItem)
	a.Equal(50, output.StockItem.PK)
	a.True(fake.createdStockItem)
	a.Equal(40, fake.lastGetStockLocationID)
	a.Equal(inventree.StockItemQuery{PartID: 10, LocationID: 40, Limit: DefaultLookupLimit}, fake.lastSearchStockItemsQuery)
	a.Equal(inventree.StockItemCreate{Part: 10, Location: 40, Quantity: 7, Batch: dvgoutils.Ptr("B-1"), Notes: dvgoutils.Ptr("initial stock")}, fake.lastCreateStockItem)
}

func TestInitialStockWorkflowClarifiesAmbiguousInputsAndDuplicates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	fake := &fakeMilestoneLookupClient{
		parts: []inventree.Part{
			{PK: 10, Name: "10k resistor"},
			{PK: 11, Name: "10k resistor precision"},
		},
	}

	_, output, err := initialStockWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, InitialStockWorkflowInput{PartSearch: "10k", LocationID: 40, Quantity: 1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("part", output.Clarification.Field)
	a.False(fake.createdStockItem)

	fake = &fakeMilestoneLookupClient{
		stockItems: []inventree.StockItem{{PK: 50, Part: 10, Location: &locationID, Quantity: 2}},
	}
	_, output, err = initialStockWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, InitialStockWorkflowInput{PartID: 10, LocationID: locationID, Quantity: 1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("stock_item", output.Clarification.Field)
	a.Equal("stock_item_id", output.Clarification.Retry)
	a.False(fake.createdStockItem)

	_, output, err = initialStockWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, InitialStockWorkflowInput{PartID: 10, LocationID: 40})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("quantity", output.Clarification.Field)
}

func TestCreatePartAsksBeforeDuplicateCreate(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		parts: []inventree.Part{{PK: 10, Name: "10k resistor"}},
	}

	result, output, err := createPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreatePartInput{Name: "10k resistor", CategoryID: 20})

	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("part_id", output.Clarification.Retry)
	a.Equal("10", output.Clarification.Candidates[0].ID)
	a.False(fake.createdPart)
}

func TestCreatePartAsksWhenCategoryMissing(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	result, output, err := createPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreatePartInput{Name: "10k resistor"})

	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("category_id", output.Clarification.Field)
	a.Equal("category_id", output.Clarification.Retry)
	a.True(output.Clarification.HardError)
	a.False(fake.createdPart)

	_, output, err = createPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreatePartInput{Name: "10k resistor", CategoryID: -1})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("category_id", output.Clarification.Field)
	a.False(fake.createdPart)

	_, output, err = createPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreatePartInput{Name: "10k resistor", CategoryID: 20, DefaultLocation: dvgoutils.Ptr(-1)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("default_location_id", output.Clarification.Field)
	a.False(fake.createdPart)
}

func TestCreatePartPassesExplicitFalseValues(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := createPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreatePartInput{
		Name:         "10k resistor",
		CategoryID:   20,
		Purchaseable: dvgoutils.Ptr(false),
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(fake.createdPart)
	a.Equal(inventree.PartCreate{Name: "10k resistor", Category: dvgoutils.Ptr(20), Purchaseable: dvgoutils.Ptr(false)}, fake.lastCreatePart)
}

func TestUpdatePartPatchPreservesExplicitEmptyAndFalse(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}
	empty := ""
	active := false

	_, output, err := updatePart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartInput{ID: 10, Description: &empty, Active: &active})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(inventree.PatchFields{"description": inventree.Set(""), "active": inventree.Set(false)}, fake.lastUpdatePartFields)
}

func TestUpdatePartAsksWhenNoPatchFieldsProvided(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	result, output, err := updatePart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartInput{ID: 10})

	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("part", output.Clarification.Field)
	a.Equal("id", output.Clarification.Retry)
	a.Nil(fake.lastUpdatePartFields)
}

func TestUpdatePartAsksForPositiveIDFields(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}
	name := "resistor"

	_, output, err := updatePart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartInput{ID: -1, Name: &name})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("part", output.Clarification.Field)
	a.Nil(fake.lastUpdatePartFields)

	_, output, err = updatePart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartInput{ID: 10, CategoryID: dvgoutils.Ptr(-1)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("category_id", output.Clarification.Field)
	a.Nil(fake.lastUpdatePartFields)

	_, output, err = updatePart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpdatePartInput{ID: 10, DefaultLocation: dvgoutils.Ptr(-1)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("default_location_id", output.Clarification.Field)
	a.Nil(fake.lastUpdatePartFields)
}

func TestCreateCompanyAsksBeforeDuplicateAndOmitsCustomerRole(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		companies: []inventree.Company{{PK: 30, Name: "Acme", IsSupplier: true}},
	}

	_, output, err := createCompany(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateCompanyInput{Name: "Acme", Currency: "AUD", IsSupplier: true})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("company_id", output.Clarification.Retry)
	a.False(fake.createdCompany)

	fake = &fakeMilestoneLookupClient{}
	_, output, err = createCompany(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateCompanyInput{Name: "NewCo", Currency: "AUD", IsSupplier: true, IsManufacturer: true})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(inventree.CompanyCreate{Name: "NewCo", Currency: "AUD", IsSupplier: true, IsManufacturer: true}, fake.lastCreateCompany)
}

func TestCreateCompanyAsksForSupportedRoleAndCurrency(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := createCompany(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateCompanyInput{Name: "NeutralCo", Currency: "AUD"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("company", output.Clarification.Field)
	a.True(output.Clarification.HardError)
	a.False(fake.createdCompany)

	_, output, err = createCompany(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateCompanyInput{Name: "SupplierCo", IsSupplier: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("currency", output.Clarification.Field)
	a.True(output.Clarification.HardError)
	a.False(fake.createdCompany)
}

func TestCreateSupplierAndManufacturerPartsAskBeforeDuplicate(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fakeSupplier := &fakeMilestoneLookupClient{
		supplierParts: []inventree.SupplierPart{{PK: 40, Part: 10, Supplier: 30, SKU: "SKU-1"}},
	}
	_, supplierOutput, err := createSupplierPart(depsForFake(fakeSupplier))(ctx, &mcp.CallToolRequest{}, CreateSupplierPartInput{PartID: 10, SupplierID: 30, SKU: "SKU-1"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, supplierOutput.Status)
	a.Equal("supplier_part_id", supplierOutput.Clarification.Retry)
	a.Equal(inventree.SupplierPartQuery{Part: 10, Supplier: 30, SKU: "SKU-1"}, fakeSupplier.lastSearchSupplierPartsQuery)
	a.False(fakeSupplier.createdSupplierPart)

	fakeManufacturer := &fakeMilestoneLookupClient{
		manufacturerParts: []inventree.ManufacturerPart{{PK: 50, Part: 10, Manufacturer: 31, MPN: "MPN-1"}},
	}
	_, manufacturerOutput, err := createManufacturerPart(depsForFake(fakeManufacturer))(ctx, &mcp.CallToolRequest{}, CreateManufacturerPartInput{PartID: 10, ManufacturerID: 31, MPN: dvgoutils.Ptr("MPN-1")})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, manufacturerOutput.Status)
	a.Equal("manufacturer_part_id", manufacturerOutput.Clarification.Retry)
	a.Equal(inventree.ManufacturerPartQuery{Part: 10, Manufacturer: 31, MPN: "MPN-1"}, fakeManufacturer.lastSearchManufacturerPartsQuery)
	a.False(fakeManufacturer.createdManufacturerPart)
}

func TestCreateManufacturerPartNormalizesBlankOptionalMPN(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, output, err := createManufacturerPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateManufacturerPartInput{PartID: 10, ManufacturerID: 31, MPN: dvgoutils.Ptr("  \t ")})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(inventree.ManufacturerPartQuery{Part: 10, Manufacturer: 31}, fake.lastSearchManufacturerPartsQuery)
	a.Nil(fake.lastCreateManufacturerPart.MPN)
}

func TestCreateManufacturerPartPreservesNonblankMPNExactly(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}
	mpn := " ABC 123 "

	_, output, err := createManufacturerPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateManufacturerPartInput{PartID: 10, ManufacturerID: 31, MPN: &mpn})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(inventree.ManufacturerPartQuery{Part: 10, Manufacturer: 31, MPN: mpn}, fake.lastSearchManufacturerPartsQuery)
	a.Equal(&mpn, fake.lastCreateManufacturerPart.MPN)
}

func TestCreateManufacturerPartReturnsSafeValidationDetails(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{createManufacturerPartErr: &inventree.APIError{
		StatusCode: http.StatusBadRequest,
		Kind:       inventree.ErrorKindValidation,
		FieldErrors: map[string][]string{
			"MPN":    {"This field may not be blank."},
			"link":   {"Invalid URL https://operator:secret@example.test/?token=private"},
			"tax_id": {"private-tax-value"},
		},
	}}

	_, output, err := createManufacturerPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateManufacturerPartInput{PartID: 10, ManufacturerID: 31, MPN: dvgoutils.Ptr("MPN-1")})

	r.NoError(err)
	a.Equal(StatusValidationFailed, output.Status)
	r.NotNil(output.Validation)
	a.Equal(http.StatusBadRequest, output.Validation.StatusCode)
	a.Equal([]ValidationFieldError{
		{Field: "link", Messages: []string{"Enter a valid URL."}},
		{Field: "MPN", Messages: []string{"This field may not be blank."}},
	}, output.Validation.Fields)
	a.NotContains(fmt.Sprint(output.Validation), "secret")
	a.NotContains(fmt.Sprint(output.Validation), "private")
	a.NotContains(fmt.Sprint(output.Validation), "tax_id")
}

func TestUpsertPartWorkflowPreservesCreatedIDsAfterLaterValidationFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{
		manufacturers: []inventree.Company{{PK: 31, Name: "PartsCo", IsManufacturer: true}},
		suppliers:     []inventree.Company{{PK: 30, Name: "Acme", IsSupplier: true}},
		createSupplierPartErr: &inventree.APIError{
			StatusCode:  http.StatusBadRequest,
			Kind:        inventree.ErrorKindValidation,
			FieldErrors: map[string][]string{"SKU": {"This field is invalid: secret-value"}},
		},
	}

	_, output, err := upsertPartWorkflow(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, UpsertPartWorkflowInput{
		Name:             "live order resistor",
		CategoryID:       20,
		ManufacturerName: "PartsCo",
		MPN:              dvgoutils.Ptr("MPN-1"),
		SupplierName:     "Acme",
		SupplierSKU:      "ORDER-SKU-1",
	})

	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.Part)
	a.Equal(10, output.Part.PK)
	r.NotNil(output.ManufacturerPart)
	a.Equal(50, output.ManufacturerPart.PK)
	r.NotNil(output.Supplier)
	a.Equal(30, output.Supplier.PK)
	r.NotNil(output.Failure)
	a.Equal("create_supplier_part", output.Failure.Action)
	r.NotNil(output.Failure.Validation)
	a.Equal("Search supplier parts using the returned part and supplier IDs plus the exact SKU before retrying creation.", output.Failure.RecoveryPlan)
	a.Equal([]ValidationFieldError{{Field: "SKU", Messages: []string{"Rejected by InvenTree."}}}, output.Failure.Validation.Fields)
	a.Empty(output.RemainingActions)
	a.Contains(output.Actions, PartUpsertWorkflowAction{Name: "create_part", Status: "created", RecordType: "part", ID: 10, Reason: "no matching part found"})
	a.Contains(output.Actions, PartUpsertWorkflowAction{Name: "create_manufacturer_part", Status: "created", RecordType: "manufacturerpart", ID: 50, Reason: "no matching manufacturer-part found"})
	a.Contains(output.Actions, PartUpsertWorkflowAction{Name: "create_supplier_part", Status: "failed", RecordType: "supplierpart", Reason: "InvenTree mutation did not return a verified result"})
	a.Equal(dvgoutils.Ptr("MPN-1"), fake.lastCreateManufacturerPart.MPN)
	a.NotContains(fmt.Sprint(output), "secret-value")
}

func TestCreateSupplierAndManufacturerPartsAskForPositiveIDs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeMilestoneLookupClient{}

	_, supplierOutput, err := createSupplierPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateSupplierPartInput{PartID: 0, SupplierID: 30, SKU: "SKU-1"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, supplierOutput.Status)
	a.Equal("part", supplierOutput.Clarification.Field)
	a.True(supplierOutput.Clarification.HardError)
	a.False(fake.createdSupplierPart)

	_, supplierOutput, err = createSupplierPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateSupplierPartInput{PartID: 10, SupplierID: 0, SKU: "SKU-1"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, supplierOutput.Status)
	a.Equal("supplier", supplierOutput.Clarification.Field)
	a.True(supplierOutput.Clarification.HardError)
	a.False(fake.createdSupplierPart)

	_, supplierOutput, err = createSupplierPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateSupplierPartInput{PartID: 10, SupplierID: 30, SKU: "SKU-1", ManufacturerPartID: dvgoutils.Ptr(-1)})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, supplierOutput.Status)
	a.Equal("manufacturer_part_id", supplierOutput.Clarification.Field)
	a.True(supplierOutput.Clarification.HardError)
	a.False(fake.createdSupplierPart)

	_, manufacturerOutput, err := createManufacturerPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateManufacturerPartInput{PartID: 0, ManufacturerID: 31})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, manufacturerOutput.Status)
	a.Equal("part", manufacturerOutput.Clarification.Field)
	a.True(manufacturerOutput.Clarification.HardError)
	a.False(fake.createdManufacturerPart)

	_, manufacturerOutput, err = createManufacturerPart(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, CreateManufacturerPartInput{PartID: 10, ManufacturerID: 0})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, manufacturerOutput.Status)
	a.Equal("manufacturer", manufacturerOutput.Clarification.Field)
	a.True(manufacturerOutput.Clarification.HardError)
	a.False(fake.createdManufacturerPart)
}

func TestSetPartParametersUpdatesExistingAndCreatesMissing(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	zero := 0.0
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameters: []inventree.Parameter{
			{PK: 60, Template: 70, ModelType: "part.part", ModelID: 10, Data: "old"},
		},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: 70, Name: "Resistance", Units: dvgoutils.Ptr("ohm"), Choices: "0,10k", Enabled: true},
			{PK: 71, Name: "Tolerance", Units: dvgoutils.Ptr("%"), Enabled: true},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: 70},
			{PK: 81, Category: categoryID, Template: 71},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID: 10,
		Parameters: []ParameterSetInput{
			{Name: "Resistance", NumberValue: &zero},
			{Name: "Tolerance", Value: dvgoutils.Ptr("")},
		},
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	r.Len(output.Record, 2)
	a.Equal(inventree.CategoryParameterTemplateQuery{CategoryID: categoryID}, fake.lastSearchCategoryParameterTemplatesQuery)
	a.Equal(inventree.PartParameterQuery{PartID: 10}, fake.lastSearchPartParametersQuery)
	a.Equal(inventree.PatchFields{"data": inventree.Set("0")}, fake.lastUpdatePartParameterFields)
	a.Equal(inventree.NewPartParameter(10, 71, ""), fake.lastCreatePartParameter)
}

func TestSetPartParametersPreservesExplicitFalse(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	falseValue := false
	templateID := 70
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: templateID},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID:     10,
		Parameters: []ParameterSetInput{{TemplateID: &templateID, BoolValue: &falseValue}},
	})

	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(inventree.NewPartParameter(10, templateID, "false"), fake.lastCreatePartParameter)
	a.Equal(templateID, fake.lastGetParameterTemplateID)
}

func TestSetPartParametersAsksForAmbiguousTemplate(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameters: []inventree.Parameter{
			{PK: 60, Template: 70, ModelType: "part.part", ModelID: 10, Data: "old"},
		},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: 70, Name: "Resistance", Units: dvgoutils.Ptr("ohm"), Enabled: true},
			{PK: 71, Name: "Resistance", Units: dvgoutils.Ptr("kohm"), Enabled: true},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: 70, DefaultValue: "10k"},
			{PK: 81, Category: categoryID, Template: 71},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID:     10,
		Parameters: []ParameterSetInput{{Name: "Resistance", Value: dvgoutils.Ptr("10k")}},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("template_id", output.Clarification.Retry)
	a.Len(output.Clarification.Candidates, 2)
	a.Equal(true, output.Clarification.Candidates[0].Fields["enabled"])
	a.Equal(true, output.Clarification.Candidates[0].Fields["category_linked"])
	a.Equal(80, output.Clarification.Candidates[0].Fields["category_link_id"])
	a.Equal("old", output.Clarification.Candidates[0].Fields["existing_value"])
	a.False(fake.createdPartParameter)
	a.Nil(fake.lastUpdatePartParameterFields)
}

func TestSetPartParametersRefusesDisabledOrUnlinkedTemplates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: 70, Name: "Resistance", Enabled: false},
			{PK: 71, Name: "Resistance", Enabled: true},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: 70},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID:     10,
		Parameters: []ParameterSetInput{{Name: "Resistance", Value: dvgoutils.Ptr("10k")}},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("template", output.Clarification.Field)
	a.True(output.Clarification.HardError)
	a.False(fake.createdPartParameter)
	a.Nil(fake.lastUpdatePartParameterFields)

	templateID := 71
	_, output, err = setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID:     10,
		Parameters: []ParameterSetInput{{TemplateID: &templateID, Value: dvgoutils.Ptr("10k")}},
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("template_id", output.Clarification.Field)
	a.True(output.Clarification.HardError)
}

func TestSetPartParametersRefusesDisabledTemplateID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	templateID := 70
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: templateID, Name: "Resistance", Enabled: false},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: templateID},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID:     10,
		Parameters: []ParameterSetInput{{TemplateID: &templateID, Value: dvgoutils.Ptr("10k")}},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("template_id", output.Clarification.Field)
	a.True(output.Clarification.HardError)
	a.False(fake.createdPartParameter)
}

func TestSetPartParametersPreflightsBeforeWriting(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: 70, Name: "Resistance", Enabled: true},
			{PK: 71, Name: "Tolerance", Enabled: true},
			{PK: 72, Name: "Tolerance", Enabled: true},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: 70},
			{PK: 81, Category: categoryID, Template: 71},
			{PK: 82, Category: categoryID, Template: 72},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID: 10,
		Parameters: []ParameterSetInput{
			{Name: "Resistance", Value: dvgoutils.Ptr("10k")},
			{Name: "Tolerance", Value: dvgoutils.Ptr("1%")},
		},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("template_id", output.Clarification.Retry)
	a.False(fake.createdPartParameter)
	a.Zero(fake.updatePartParameterCount)
}

func TestSetPartParametersRejectsDuplicateTemplatesBeforeWriting(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	fake := &fakeMilestoneLookupClient{
		part: inventree.Part{PK: 10, Category: &categoryID},
		parameterTemplates: []inventree.ParameterTemplate{
			{PK: 70, Name: "Resistance", Enabled: true},
		},
		categoryParameterTemplates: []inventree.CategoryParameterTemplate{
			{PK: 80, Category: categoryID, Template: 70},
		},
	}

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{
		PartID: 10,
		Parameters: []ParameterSetInput{
			{Name: "Resistance", Value: dvgoutils.Ptr("10k")},
			{Name: "Resistance", Value: dvgoutils.Ptr("22k")},
		},
	})

	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	r.NotNil(output.Clarification)
	a.Equal("template_id", output.Clarification.Field)
	a.True(output.Clarification.HardError)
	a.Zero(fake.createPartParameterCount)
	a.Zero(fake.updatePartParameterCount)
}

func TestSetPartParametersAsksForInvalidInputs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	categoryID := 20
	fake := &fakeMilestoneLookupClient{part: inventree.Part{PK: 10, Category: &categoryID}}
	value := "10k"
	falseValue := false

	_, output, err := setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{PartID: 0, Parameters: []ParameterSetInput{{Value: &value}}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("part", output.Clarification.Field)

	_, output, err = setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{PartID: 10})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("parameters", output.Clarification.Field)

	_, output, err = setPartParameters(depsForFake(fake))(ctx, &mcp.CallToolRequest{}, SetPartParametersInput{PartID: 10, Parameters: []ParameterSetInput{{Name: "Resistance", Value: &value, BoolValue: &falseValue}}})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("value", output.Clarification.Field)
	a.False(fake.createdPartParameter)
}
