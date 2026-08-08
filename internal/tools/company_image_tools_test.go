package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetCompanyImageAssignsAndVerifiesExactDigest(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	content := companyPNG(t, color.NRGBA{R: 20, G: 40, B: 60, A: 255})
	fake := newFakeCompanyImageClient(nil)

	_, output, err := setCompanyImage(companyImageDeps(fake))(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
		CompanyID: 30, InlineBase64: encodeBase64(content), Filename: "logo.png", ContentType: "image/png",
	})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.Verified)
	a.False(output.Replaced)
	a.False(output.Recovered)
	r.NotNil(output.Image)
	a.Equal(30, output.Image.CompanyID)
	a.Equal(int64(len(content)), output.Image.Size)
	a.Equal(2, output.Image.Width)
	a.Equal(3, output.Image.Height)
	a.Len(output.Image.SHA256, 64)
	a.Equal("/media/company_images/company_30_img.png", output.Image.ImageURL)
	a.True(fake.company.IsSupplier)
	a.True(fake.company.IsCustomer)
	a.Equal(4, fake.company.PartsSupplied)
	a.Equal(5, fake.company.PartsManufactured)
	a.Equal("private note", *fake.company.Notes)
}

func TestSetCompanyImageRequiresReplacementConfirmation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	old := companyPNG(t, color.NRGBA{B: 255, A: 255})
	content := companyPNG(t, color.NRGBA{R: 255, A: 255})
	fake := newFakeCompanyImageClient(old)

	_, output, err := setCompanyImage(companyImageDeps(fake))(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
		CompanyID: 30, InlineBase64: encodeBase64(content), Filename: "logo.png", ContentType: "image/png",
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.True(output.Replaced)
	a.Zero(fake.setCalls)
	r.NotNil(output.Clarification)
	a.Equal("confirm", output.Clarification.Retry)

	_, output, err = setCompanyImage(companyImageDeps(fake))(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
		CompanyID: 30, InlineBase64: encodeBase64(content), Filename: "logo.png", ContentType: "image/png", Confirm: true,
	})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.Replaced)
	a.Equal(1, fake.setCalls)
	a.Equal(content, fake.content)
}

func TestSetCompanyImageRecoversOnlyExactAmbiguousDigest(t *testing.T) {
	t.Parallel()
	content := companyPNG(t, color.NRGBA{G: 255, A: 255})
	tests := []struct {
		name          string
		applyOnError  bool
		wantStatus    string
		wantRecovered bool
	}{
		{name: "applied", applyOnError: true, wantStatus: StatusOK, wantRecovered: true},
		{name: "not applied", applyOnError: false, wantStatus: StatusPartialFailure, wantRecovered: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeCompanyImageClient(nil)
			fake.setErr = context.DeadlineExceeded
			fake.applySetOnError = tt.applyOnError
			_, output, err := setCompanyImage(companyImageDeps(fake))(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
				CompanyID: 30, InlineBase64: encodeBase64(content), Filename: "logo.png", ContentType: "image/png",
			})
			r.NoError(err)
			a.Equal(tt.wantStatus, output.Status)
			a.Equal(tt.wantRecovered, output.Recovered)
			if tt.wantStatus == StatusOK {
				a.True(output.Verified)
			} else {
				a.Contains(output.RecoveryPlan, "do not retry blindly")
			}
		})
	}
}

func TestSetCompanyImageReturnsSanitizedValidationAndRejectsSourceConfusion(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	content := companyPNG(t, color.NRGBA{A: 255})
	fake := newFakeCompanyImageClient(nil)
	fake.setErr = &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation, FieldErrors: map[string][]string{"image": {"invalid secret /tmp/logo.png"}, "tax_id": {"secret"}}}

	_, output, err := setCompanyImage(companyImageDeps(fake))(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
		CompanyID: 30, InlineBase64: encodeBase64(content), Filename: "logo.png", ContentType: "image/png",
	})
	r.NoError(err)
	a.Equal(StatusValidationFailed, output.Status)
	r.NotNil(output.Validation)
	a.Equal([]ValidationFieldError{{Field: "image", Messages: []string{"Rejected by InvenTree."}}}, output.Validation.Fields)

	_, output, err = setCompanyImage(companyImageDeps(newFakeCompanyImageClient(nil)))(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{CompanyID: 30, LocalPath: "https://example.test/logo.png"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("url", output.Clarification.Retry)
}

func TestCompanyImageInputsRequireStableIdentityAndOneUsableSource(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeCompanyImageClient(nil)
	deps := companyImageDeps(fake)

	_, setID, err := setCompanyImage(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, setID.Status)
	_, urlID, err := setCompanyImageFromURL(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageFromURLInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, urlID.Status)
	_, clearID, err := clearCompanyImage(deps)(ctx, &mcp.CallToolRequest{}, ClearCompanyImageInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, clearID.Status)

	_, missing, err := setCompanyImage(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{CompanyID: 30})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, missing.Status)
	_, multiple, err := setCompanyImage(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{CompanyID: 30, InlineBase64: "eA==", LocalPath: "/tmp/logo.png"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, multiple.Status)
	_, _, err = setCompanyImage(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{CompanyID: 30, InlineBase64: "%%%", Filename: "logo.png", ContentType: "image/png"})
	r.ErrorContains(err, "not valid base64")

	deps.UploadMaxBytes = 1
	_, _, err = setCompanyImage(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{CompanyID: 30, InlineBase64: "eHg=", Filename: "logo.png", ContentType: "image/png"})
	r.ErrorContains(err, "exceeds company image maximum bytes")

	content := companyPNG(t, color.NRGBA{R: 1, A: 255})
	deps = companyImageDeps(fake)
	_, missingFilename, err := setCompanyImage(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{CompanyID: 30, InlineBase64: encodeBase64(content), ContentType: "image/png"})
	r.NoError(err)
	a.Equal("filename", missingFilename.Clarification.Retry)
	_, missingType, err := setCompanyImage(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{CompanyID: 30, InlineBase64: encodeBase64(content), Filename: "logo.png"})
	r.NoError(err)
	a.Equal("content_type", missingType.Clarification.Retry)
}

func TestCompanyImagePreflightRequiresExactFreshIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	content := companyPNG(t, color.NRGBA{R: 255, A: 255})

	mismatch := newFakeCompanyImageClient(nil)
	mismatch.company.PK = 31
	_, _, err := setCompanyImage(companyImageDeps(mismatch))(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
		CompanyID: 30, InlineBase64: encodeBase64(content), Filename: "logo.png", ContentType: "image/png",
	})
	r.ErrorContains(err, "mismatched identity")
	a.Zero(mismatch.setCalls)

	stale := newFakeCompanyImageClient(nil)
	stale.imageOnGetCall = 2
	stale.imageOnGetContent = companyPNG(t, color.NRGBA{B: 255, A: 255})
	_, output, err := setCompanyImage(companyImageDeps(stale))(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
		CompanyID: 30, InlineBase64: encodeBase64(content), Filename: "logo.png", ContentType: "image/png",
	})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("confirm", output.Clarification.Retry)
	a.Zero(stale.setCalls)
	a.Equal(2, stale.getCalls)
}

func TestSetCompanyImageFromURLUsesDedicatedFetchPolicy(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	content := companyPNG(t, color.NRGBA{R: 120, A: 255})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		a.Empty(req.Header.Get("Authorization"))
		if req.URL.Path != "/missing-type" {
			w.Header().Set("Content-Type", "image/png")
		} else {
			w.Header().Set("Content-Type", "")
		}
		w.Header().Set("Content-Disposition", `attachment; filename="supplier.png"`)
		_, _ = w.Write(content)
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	r.NoError(err)
	fake := newFakeCompanyImageClient(nil)
	deps := companyImageDeps(fake)
	deps.URLFetcher = upload.URLFetcher{
		Resolver: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		Allowlist: []upload.URLAllowlistEntry{{Scheme: parsed.Scheme, Host: parsed.Hostname(), Port: parsed.Port()}},
	}

	_, output, err := setCompanyImageFromURL(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageFromURLInput{CompanyID: 30, URL: server.URL + "/supplier.png"})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(content, fake.content)
	a.Equal("supplier.png", fake.filename)

	missingTypeFake := newFakeCompanyImageClient(nil)
	deps.ClientFromContext = func(context.Context) (any, error) { return missingTypeFake, nil }
	_, missingType, err := setCompanyImageFromURL(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageFromURLInput{CompanyID: 30, URL: server.URL + "/missing-type"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, missingType.Status)
	a.Equal("content_type", missingType.Clarification.Retry)
	a.Zero(missingTypeFake.setCalls)

	overrideFake := newFakeCompanyImageClient(nil)
	deps.ClientFromContext = func(context.Context) (any, error) { return overrideFake, nil }
	_, overridden, err := setCompanyImageFromURL(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageFromURLInput{CompanyID: 30, URL: server.URL + "/missing-type", ContentType: "image/png"})
	r.NoError(err)
	a.Equal(StatusOK, overridden.Status)
	a.Equal(content, overrideFake.content)
}

func TestSetCompanyImageUsesAllowlistedLocalFileOnlyInStdioMode(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	content := companyPNG(t, color.NRGBA{G: 120, A: 255})
	fs := afero.NewMemMapFs()
	r.NoError(fs.MkdirAll("/allowed", 0o755))
	r.NoError(afero.WriteFile(fs, "/allowed/logo.png", content, 0o600))
	fake := newFakeCompanyImageClient(nil)
	deps := companyImageDeps(fake)
	deps.UploadFS = fs
	deps.UploadAllowRoots = []string{"/allowed"}

	_, output, err := setCompanyImage(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{CompanyID: 30, LocalPath: "/allowed/logo.png", ContentType: "image/png"})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal("logo.png", fake.filename)

	httpFake := newFakeCompanyImageClient(nil)
	deps = companyImageDeps(httpFake)
	deps.UploadMode = upload.ModeHTTP
	deps.UploadFS = fs
	deps.UploadAllowRoots = []string{"/allowed"}
	_, _, err = setCompanyImage(deps)(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{CompanyID: 30, LocalPath: "/allowed/logo.png", ContentType: "image/png"})
	r.ErrorContains(err, "HTTP mode rejects local upload paths")
	a.Zero(httpFake.setCalls)
}

func TestClearCompanyImageRequiresConfirmationAndRecoversNullReadback(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeCompanyImageClient(companyPNG(t, color.NRGBA{B: 255, A: 255}))

	_, output, err := clearCompanyImage(companyImageDeps(fake))(ctx, &mcp.CallToolRequest{}, ClearCompanyImageInput{CompanyID: 30})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Zero(fake.clearCalls)

	fake.clearErr = context.DeadlineExceeded
	fake.applyClearOnError = true
	_, output, err = clearCompanyImage(companyImageDeps(fake))(ctx, &mcp.CallToolRequest{}, ClearCompanyImageInput{CompanyID: 30, Confirm: true})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.True(output.Recovered)
	a.True(output.Verified)
	a.Nil(fake.company.Image)

	_, output, err = clearCompanyImage(companyImageDeps(fake))(ctx, &mcp.CallToolRequest{}, ClearCompanyImageInput{CompanyID: 30, Confirm: true})
	r.NoError(err)
	a.Equal(StatusNoImage, output.Status)
}

func TestCompanyImageRefusesUnprovedAssignmentAndClearResults(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	old := companyPNG(t, color.NRGBA{B: 255, A: 255})
	replacement := companyPNG(t, color.NRGBA{R: 255, A: 255})

	roleChanged := newFakeCompanyImageClient(old)
	roleChanged.mutateRoleOnSet = true
	_, assignment, err := setCompanyImage(companyImageDeps(roleChanged))(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
		CompanyID: 30, InlineBase64: encodeBase64(replacement), Filename: "replacement.png", ContentType: "image/png", Confirm: true,
	})
	r.NoError(err)
	a.Equal(StatusPartialFailure, assignment.Status)
	a.Contains(assignment.RecoveryPlan, "do not retry blindly")

	notApplied := newFakeCompanyImageClient(old)
	notApplied.clearErr = context.DeadlineExceeded
	_, cleared, err := clearCompanyImage(companyImageDeps(notApplied))(ctx, &mcp.CallToolRequest{}, ClearCompanyImageInput{CompanyID: 30, Confirm: true})
	r.NoError(err)
	a.Equal(StatusPartialFailure, cleared.Status)
	a.False(cleared.Recovered)
	a.Contains(cleared.RecoveryPlan, "do not retry blindly")

	divergent := newFakeCompanyImageClient(old)
	divergent.setErr = context.DeadlineExceeded
	divergent.applySetOnError = true
	divergent.downloadContentOverride = old
	_, divergentOutput, err := setCompanyImage(companyImageDeps(divergent))(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
		CompanyID: 30, InlineBase64: encodeBase64(replacement), Filename: "replacement.png", ContentType: "image/png", Confirm: true,
	})
	r.NoError(err)
	a.Equal(StatusPartialFailure, divergentOutput.Status)
	a.False(divergentOutput.Verified)
	a.False(divergentOutput.Recovered)
	r.NotNil(divergentOutput.Current)
	a.True(divergentOutput.Current.HasImage)
	a.Equal(fmt.Sprintf("%x", sha256.Sum256(old)), divergentOutput.Current.SHA256)
	a.Contains(divergentOutput.RecoveryPlan, "do not retry blindly")

	mismatchedSet := newFakeCompanyImageClient(nil)
	mismatchedSet.setResponsePK = 31
	_, mismatchedSetOutput, err := setCompanyImage(companyImageDeps(mismatchedSet))(ctx, &mcp.CallToolRequest{}, SetCompanyImageInput{
		CompanyID: 30, InlineBase64: encodeBase64(replacement), Filename: "replacement.png", ContentType: "image/png",
	})
	r.NoError(err)
	a.Equal(StatusPartialFailure, mismatchedSetOutput.Status)

	mismatchedClear := newFakeCompanyImageClient(old)
	mismatchedClear.clearResponsePK = 31
	_, mismatchedClearOutput, err := clearCompanyImage(companyImageDeps(mismatchedClear))(ctx, &mcp.CallToolRequest{}, ClearCompanyImageInput{CompanyID: 30, Confirm: true})
	r.NoError(err)
	a.Equal(StatusPartialFailure, mismatchedClearOutput.Status)

	validationClear := newFakeCompanyImageClient(old)
	validationClear.clearErr = &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation, FieldErrors: map[string][]string{"image": {"secret rejected"}}}
	_, validationOutput, err := clearCompanyImage(companyImageDeps(validationClear))(ctx, &mcp.CallToolRequest{}, ClearCompanyImageInput{CompanyID: 30, Confirm: true})
	r.NoError(err)
	a.Equal(StatusValidationFailed, validationOutput.Status)
	r.NotNil(validationOutput.Validation)
}

func TestCompanyImageToolAuthorizations(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeUpload}, ToolAuthorizations[SetCompanyImageToolName].Scopes)
	a.Equal(WriteAnnotations, ToolAuthorizations[SetCompanyImageToolName].Annotations)
	urlAnnotations := WriteAnnotations
	urlAnnotations.OpenWorld = true
	a.Equal(urlAnnotations, ToolAuthorizations[SetCompanyImageFromURLToolName].Annotations)
	a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeUpload, ScopeInventreeDestructive}, ToolAuthorizations[ClearCompanyImageToolName].Scopes)
	a.True(ToolAuthorizations[ClearCompanyImageToolName].Annotations.Destructive)
}

func TestSetCompanyImageThroughMCPBoundary(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	content := companyPNG(t, color.NRGBA{R: 30, G: 60, B: 90, A: 255})
	urlContent := companyPNG(t, color.NRGBA{R: 90, G: 60, B: 30, A: 255})
	urlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Disposition", `attachment; filename="url-logo.png"`)
		_, _ = w.Write(urlContent)
	}))
	t.Cleanup(urlServer.Close)
	parsedURL, err := url.Parse(urlServer.URL)
	r.NoError(err)
	fake := newFakeCompanyImageClient(nil)
	deps := companyImageDeps(fake)
	deps.EnableWriteTools = true
	deps.URLFetcher = upload.URLFetcher{
		Resolver: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		Allowlist: []upload.URLAllowlistEntry{{Scheme: parsedURL.Scheme, Host: parsedURL.Hostname(), Port: parsedURL.Port()}},
	}

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		server := mcp.NewServer(&mcp.Implementation{Name: "company-image-test-server", Version: "v0.0.0"}, nil)
		Register(server, deps)
		serverDone <- server.Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "company-image-test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
		cancel()
		<-serverDone
	}()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: SetCompanyImageToolName, Arguments: map[string]any{
		"company_id": 30, "inline_base64": encodeBase64(content), "filename": "logo.png", "content_type": "image/png",
	}})
	r.NoError(err)
	a.False(result.IsError)
	structured := result.StructuredContent.(map[string]any)
	a.Equal(StatusOK, structured["status"])
	a.Equal(true, structured["verified"])
	image := structured["image"].(map[string]any)
	a.Equal(float64(30), image["company_id"])
	a.Equal(fmt.Sprintf("%x", sha256.Sum256(content)), image["sha256"])

	clearedResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: ClearCompanyImageToolName, Arguments: map[string]any{"company_id": 30, "confirm": true}})
	r.NoError(err)
	a.False(clearedResult.IsError)
	cleared := clearedResult.StructuredContent.(map[string]any)
	a.Equal(StatusOK, cleared["status"])
	a.Equal(false, cleared["recovered"])
	a.Equal(true, cleared["verified"])
	current := cleared["current"].(map[string]any)
	a.Equal(false, current["has_image"])

	urlResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: SetCompanyImageFromURLToolName, Arguments: map[string]any{"company_id": 30, "url": urlServer.URL + "/url-logo.png"}})
	r.NoError(err)
	a.False(urlResult.IsError)
	urlOutput := urlResult.StructuredContent.(map[string]any)
	a.Equal(StatusOK, urlOutput["status"])
	a.Equal(false, urlOutput["replaced"])
	urlImage := urlOutput["image"].(map[string]any)
	a.Equal(fmt.Sprintf("%x", sha256.Sum256(urlContent)), urlImage["sha256"])
}

type fakeCompanyImageClient struct {
	company                 inventree.CompanyDetail
	content                 []byte
	filename                string
	contentType             string
	setErr                  error
	clearErr                error
	applySetOnError         bool
	applyClearOnError       bool
	mutateRoleOnSet         bool
	getCalls                int
	imageOnGetCall          int
	imageOnGetContent       []byte
	downloadContentOverride []byte
	setResponsePK           int
	clearResponsePK         int
	setCalls                int
	clearCalls              int
}

func newFakeCompanyImageClient(content []byte) *fakeCompanyImageClient {
	note := "private note"
	email := "private@example.test"
	fake := &fakeCompanyImageClient{company: inventree.CompanyDetail{
		Company: inventree.Company{PK: 30, Name: "Supplier", Description: "description", Currency: "AUD", Active: true, IsSupplier: true, IsCustomer: true, PartsSupplied: 4, PartsManufactured: 5},
		Website: "https://example.test", Phone: "private", Email: &email, Contact: "private", Link: "https://example.test/private", Notes: &note, TaxID: "private-tax",
	}}
	if content != nil {
		imageURL := "/media/company_images/company_30_img.png?secret=value"
		fake.company.Image = &imageURL
		fake.content = append([]byte(nil), content...)
		fake.filename = "company_30_img.png"
		fake.contentType = "image/png"
	}
	return fake
}

func (f *fakeCompanyImageClient) GetCompanyDetail(context.Context, int) (inventree.CompanyDetail, error) {
	f.getCalls++
	if f.imageOnGetCall > 0 && f.getCalls == f.imageOnGetCall {
		imageURL := "/media/company_images/company_30_concurrent.png"
		f.company.Image = &imageURL
		f.content = append([]byte(nil), f.imageOnGetContent...)
		f.filename = "company_30_concurrent.png"
		f.contentType = "image/png"
	}
	return f.company, nil
}

func (f *fakeCompanyImageClient) SetCompanyPrimaryImage(_ context.Context, id int, input inventree.CompanyPrimaryImageCreate) (inventree.CompanyDetail, error) {
	f.setCalls++
	if f.setErr == nil || f.applySetOnError {
		imageURL := "/media/company_images/company_30_img.png?secret=value"
		f.company.Image = &imageURL
		f.content = append([]byte(nil), input.Content...)
		f.filename = input.Filename
		f.contentType = input.ContentType
		if f.mutateRoleOnSet {
			f.company.IsCustomer = false
		}
	}
	response := f.company
	if f.setResponsePK != 0 {
		response.PK = f.setResponsePK
	}
	return response, f.setErr
}

func (f *fakeCompanyImageClient) ClearCompanyPrimaryImage(context.Context, int) (inventree.CompanyDetail, error) {
	f.clearCalls++
	if f.clearErr == nil || f.applyClearOnError {
		f.company.Image = nil
		f.content = nil
	}
	response := f.company
	if f.clearResponsePK != 0 {
		response.PK = f.clearResponsePK
	}
	return response, f.clearErr
}

func (f *fakeCompanyImageClient) DownloadCompanyImage(context.Context, int, int64) (inventree.DownloadedCompanyImage, error) {
	if f.company.Image == nil {
		return inventree.DownloadedCompanyImage{}, inventree.ErrCompanyImageMissing
	}
	content := f.content
	if f.downloadContentOverride != nil {
		content = f.downloadContentOverride
	}
	return inventree.DownloadedCompanyImage{Company: f.company, Content: append([]byte(nil), content...), Filename: f.filename, ContentType: f.contentType, SourceURL: redactedMetadataURL(f.company.Image)}, nil
}

func companyImageDeps(fake *fakeCompanyImageClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }, EnableWriteTools: true, UploadMode: upload.ModeStdio, UploadMaxBytes: upload.CompanyImageMaxBytes}
}

func companyPNG(t *testing.T, pixel color.NRGBA) []byte {
	t.Helper()
	r := require.New(t)
	img := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	img.SetNRGBA(0, 0, pixel)
	var out bytes.Buffer
	r.NoError(png.Encode(&out, img))
	return out.Bytes()
}

func encodeBase64(content []byte) string {
	return base64.StdEncoding.EncodeToString(content)
}
