package tools

import (
	"context"
	"encoding/base64"
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

func TestWriteToolAuthorizationsUseWriteScope(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	for _, name := range writeToolNames {
		auth, ok := ToolAuthorizations[name]
		r.True(ok, "missing authorization for %s", name)
		switch name {
		case CreateStockItemToolName, InitialStockWorkflowToolName:
			a.Equal("operational", auth.MutationClass)
			a.Equal([]string{ScopeInventreeWrite, ScopeInventreeOperational}, auth.Scopes)
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
	a.False(fake.createdPart)
	a.False(fake.createdCompany)
	a.False(fake.createdManufacturerPart)
	a.False(fake.createdSupplierPart)
	a.Contains(output.OmittedRecommendedFields, "ipn")
	a.Contains(output.OmittedRecommendedFields, "units")
	a.Contains(output.OmittedRecommendedFields, "default_location_id")
	a.NotContains(output.OmittedRecommendedFields, "purchaseable")
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
