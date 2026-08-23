package tools

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CompanyImageClient interface {
	GetCompanyDetail(context.Context, int) (inventree.CompanyDetail, error)
	SetCompanyPrimaryImage(context.Context, int, inventree.CompanyPrimaryImageCreate) (inventree.CompanyDetail, error)
	ClearCompanyPrimaryImage(context.Context, int) (inventree.CompanyDetail, error)
	DownloadCompanyImage(context.Context, int, int64) (inventree.DownloadedCompanyImage, error)
}

type SetCompanyImageInput struct {
	CompanyID    int    `json:"company_id" jsonschema:"Stable company primary key. Existing supplier, manufacturer, customer, and mixed-role companies are supported."`
	InlineBase64 string `json:"inline_base64,omitempty" jsonschema:"Base64-encoded PNG, JPEG, or WebP bytes. Mutually exclusive with local_path."`
	LocalPath    string `json:"local_path,omitempty" jsonschema:"STDIO-only allowlisted local PNG, JPEG, or WebP path. Mutually exclusive with inline_base64."`
	Filename     string `json:"filename,omitempty" jsonschema:"Required for inline bytes; optional local-path override. The .png, .jpg, .jpeg, or .webp extension must match decoded content."`
	ContentType  string `json:"content_type,omitempty" jsonschema:"Required image/png, image/jpeg, or image/webp media type for inline and local-path sources, matching decoded content."`
	Confirm      bool   `json:"confirm,omitempty" jsonschema:"Required true before replacing an existing company primary image."`
}

type SetCompanyImageFromURLInput struct {
	CompanyID   int    `json:"company_id" jsonschema:"Stable company primary key. Existing supplier, manufacturer, customer, and mixed-role companies are supported."`
	URL         string `json:"url" jsonschema:"HTTP(S) image URL fetched under the configured SSRF and redirect policy."`
	Filename    string `json:"filename,omitempty" jsonschema:"Optional filename override whose supported extension must match decoded content."`
	ContentType string `json:"content_type,omitempty" jsonschema:"Optional image/png, image/jpeg, or image/webp override when the response omits a media type; it must match decoded content."`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"Required true before replacing an existing company primary image."`
}

type ClearCompanyImageInput struct {
	CompanyID int  `json:"company_id" jsonschema:"Stable company primary key."`
	Confirm   bool `json:"confirm,omitempty" jsonschema:"Required true before clearing an existing company primary image."`
}

type CompanyImageState struct {
	inventree.WebLinkFields
	CompanyID   int    `json:"company_id"`
	HasImage    bool   `json:"has_image"`
	ImageURL    string `json:"image_url,omitempty"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
}

type CompanyImageOutput struct {
	Status              string                 `json:"status"`
	Image               *CompanyImageState     `json:"image,omitempty"`
	Current             *CompanyImageState     `json:"current,omitempty"`
	Replaced            bool                   `json:"replaced"`
	Recovered           bool                   `json:"recovered"`
	Verified            bool                   `json:"verified"`
	Validation          *ValidationFailure     `json:"validation,omitempty"`
	Clarification       *ClarificationResponse `json:"clarification,omitempty"`
	RecoveryPlan        string                 `json:"recovery_plan,omitempty"`
	LocalUploadRecovery *LocalUploadRecovery   `json:"local_upload_recovery,omitempty"`
}

func registerCompanyImageWriteTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, SetCompanyImageToolName, "Set company image", "Validates and assigns an inline or allowlisted STDIO-local company primary image with exact digest read-back. Use this dedicated company-image tool, not update_company or a generic attachment tool; replacement requires confirm:true.", setCompanyImage(deps))
	addWriteTool(server, deps, SetCompanyImageFromURLToolName, "Set company image from URL", "Fetches, validates, and assigns an HTTP(S) company primary image with exact digest read-back. Use this dedicated company-image tool, not a generic attachment URL upload; replacement requires confirm:true.", setCompanyImageFromURL(deps))
	addWriteTool(server, deps, ClearCompanyImageToolName, "Clear company image", "Explicitly clears one company's primary image and verifies exact null read-back. Use this dedicated company-image tool, not update_company or generic attachment deletion; requires confirm:true.", clearCompanyImage(deps))
}

func setCompanyImage(deps Dependencies) mcp.ToolHandlerFor[SetCompanyImageInput, CompanyImageOutput] {
	return LookupHandler[CompanyImageClient, SetCompanyImageInput, CompanyImageOutput](deps, SetCompanyImageToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyImageClient, input SetCompanyImageInput) (*mcp.CallToolResult, CompanyImageOutput, error) {
			if input.CompanyID <= 0 {
				return companyImageClarification(input.CompanyID, "Which company should receive this image?", "set_company_image requires a positive company_id", "company_id")
			}
			_, err := exactCompanyImageTarget(ctx, client, input.CompanyID)
			if err != nil {
				return nil, CompanyImageOutput{}, err
			}
			source, result, output, ok, err := resolveCompanyImageSource(ctx, deps, input)
			if err != nil || !ok {
				return result, output, err
			}
			metadata, result, output, ok, err := validateCompanyImageSource(input.CompanyID, source)
			if err != nil || !ok {
				return result, output, err
			}
			before, err := exactCompanyImageTarget(ctx, client, input.CompanyID)
			if err != nil {
				return nil, CompanyImageOutput{}, err
			}
			return assignCompanyImage(ctx, client, before, source, metadata, input.Confirm)
		})
}

func setCompanyImageFromURL(deps Dependencies) mcp.ToolHandlerFor[SetCompanyImageFromURLInput, CompanyImageOutput] {
	return LookupHandler[CompanyImageClient, SetCompanyImageFromURLInput, CompanyImageOutput](deps, SetCompanyImageFromURLToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyImageClient, input SetCompanyImageFromURLInput) (*mcp.CallToolResult, CompanyImageOutput, error) {
			if input.CompanyID <= 0 {
				return companyImageClarification(input.CompanyID, "Which company should receive this image?", "set_company_image_from_url requires a positive company_id", "company_id")
			}
			_, err := exactCompanyImageTarget(ctx, client, input.CompanyID)
			if err != nil {
				return nil, CompanyImageOutput{}, err
			}
			source, err := deps.URLFetcher.Fetch(ctx, upload.URLSource{URL: input.URL, Filename: input.Filename}, companyImageReadOptions(deps))
			if err != nil {
				return nil, CompanyImageOutput{}, err
			}
			if strings.TrimSpace(input.ContentType) != "" {
				source.ContentType = strings.TrimSpace(input.ContentType)
			}
			metadata, result, output, ok, err := validateCompanyImageSource(input.CompanyID, source)
			if err != nil || !ok {
				return result, output, err
			}
			before, err := exactCompanyImageTarget(ctx, client, input.CompanyID)
			if err != nil {
				return nil, CompanyImageOutput{}, err
			}
			return assignCompanyImage(ctx, client, before, source, metadata, input.Confirm)
		})
}

func clearCompanyImage(deps Dependencies) mcp.ToolHandlerFor[ClearCompanyImageInput, CompanyImageOutput] {
	return LookupHandler[CompanyImageClient, ClearCompanyImageInput, CompanyImageOutput](deps, ClearCompanyImageToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client CompanyImageClient, input ClearCompanyImageInput) (*mcp.CallToolResult, CompanyImageOutput, error) {
			if input.CompanyID <= 0 {
				return companyImageClarification(input.CompanyID, "Which company image should be cleared?", "clear_company_image requires a positive company_id", "company_id")
			}
			before, err := exactCompanyImageTarget(ctx, client, input.CompanyID)
			if err != nil {
				return nil, CompanyImageOutput{}, err
			}
			if !hasCompanyImage(before) {
				return TextResult(StatusNoImage), CompanyImageOutput{Status: StatusNoImage, Current: companyImageReference(before), Verified: true}, nil
			}
			if !input.Confirm {
				clarification := NewClarification("Clear the existing company primary image?", "confirm", "clear_company_image requires confirm:true before removing the image association", "confirm", true, nil, map[string]any{"company_id": input.CompanyID})
				return TextResult(StatusClarificationRequired), CompanyImageOutput{Status: StatusClarificationRequired, Current: companyImageReference(before), Clarification: &clarification}, nil
			}
			updated, mutationErr := client.ClearCompanyPrimaryImage(ctx, input.CompanyID)
			if mutationErr == nil && updated.PK != input.CompanyID {
				return companyImagePartial(before, nil, "Read the exact company ID before another write; the clear response returned a mismatched identity.")
			}
			if mutationErr != nil && !ambiguousAdminMutation(mutationErr) {
				if validation, ok := safeValidationFailure(mutationErr); ok {
					return TextResult(StatusValidationFailed), CompanyImageOutput{Status: StatusValidationFailed, Current: companyImageReference(before), Validation: validation}, nil
				}
				return nil, CompanyImageOutput{}, safeCompanyAdminError("company image clear", mutationErr)
			}
			current, readErr := client.GetCompanyDetail(ctx, input.CompanyID)
			if readErr == nil && !hasCompanyImage(current) && companyFieldsExceptImageEqual(before, current) {
				return TextResult(StatusOK), CompanyImageOutput{Status: StatusOK, Current: companyImageReference(current), Recovered: mutationErr != nil, Verified: true}, nil
			}
			return companyImagePartial(before, companyImageReference(current), "The clear result could not be proved from exact null read-back. Inspect company_id before another write; do not retry blindly.")
		})
}

func validateCompanyImageSource(companyID int, source upload.ResolvedSource) (upload.ImageMetadata, *mcp.CallToolResult, CompanyImageOutput, bool, error) {
	if strings.TrimSpace(source.Filename) == "" {
		result, output, err := companyImageClarification(companyID, "What filename and supported extension should identify this company image?", "company image assignment requires a filename whose extension matches the decoded raster", "filename")
		return upload.ImageMetadata{}, result, output, false, err
	}
	if strings.TrimSpace(source.ContentType) == "" {
		result, output, err := companyImageClarification(companyID, "What media type should identify this company image?", "company image assignment requires a content_type matching the decoded raster", "content_type")
		return upload.ImageMetadata{}, result, output, false, err
	}
	metadata, err := upload.ValidateCompanyImage(source)
	if err != nil {
		return upload.ImageMetadata{}, nil, CompanyImageOutput{}, false, err
	}
	return metadata, nil, CompanyImageOutput{}, true, nil
}

func assignCompanyImage(ctx context.Context, client CompanyImageClient, before inventree.CompanyDetail, source upload.ResolvedSource, metadata upload.ImageMetadata, confirm bool) (*mcp.CallToolResult, CompanyImageOutput, error) {
	replacing := hasCompanyImage(before)
	if replacing && !confirm {
		clarification := NewClarification("Replace the existing company primary image?", "confirm", "company image replacement requires confirm:true", "confirm", true, nil, map[string]any{"company_id": before.PK, "filename": source.Filename})
		return TextResult(StatusClarificationRequired), CompanyImageOutput{Status: StatusClarificationRequired, Current: companyImageReference(before), Replaced: true, Clarification: &clarification}, nil
	}

	updated, mutationErr := client.SetCompanyPrimaryImage(ctx, before.PK, inventree.CompanyPrimaryImageCreate{Filename: source.Filename, ContentType: metadata.ContentType, Content: source.Content})
	if mutationErr == nil && updated.PK != before.PK {
		return companyImagePartial(before, nil, "Read the exact company ID before another write; the upload response returned a mismatched identity.")
	}
	if mutationErr != nil && !ambiguousAdminMutation(mutationErr) {
		if validation, ok := safeValidationFailure(mutationErr); ok {
			return TextResult(StatusValidationFailed), CompanyImageOutput{Status: StatusValidationFailed, Current: companyImageReference(before), Replaced: replacing, Validation: validation}, nil
		}
		return nil, CompanyImageOutput{}, safeCompanyAdminError("company image upload", mutationErr)
	}

	image, current, verified := verifyCompanyImage(ctx, client, before, source, metadata)
	if verified {
		return TextResult(StatusOK), CompanyImageOutput{Status: StatusOK, Image: image, Current: image, Replaced: replacing, Recovered: mutationErr != nil, Verified: true}, nil
	}
	return companyImagePartial(before, current, "The company image content or unrelated company fields could not be verified after the write. Inspect company_id and the current image before another write; do not retry blindly.")
}

func verifyCompanyImage(ctx context.Context, client CompanyImageClient, before inventree.CompanyDetail, source upload.ResolvedSource, metadata upload.ImageMetadata) (*CompanyImageState, *CompanyImageState, bool) {
	current, err := client.GetCompanyDetail(ctx, before.PK)
	if err != nil || current.PK != before.PK || !companyFieldsExceptImageEqual(before, current) || !hasCompanyImage(current) {
		return nil, companyImageReference(current), false
	}
	download, err := client.DownloadCompanyImage(ctx, before.PK, companyImageMaxBytes(nil))
	if err != nil || download.Company.PK != before.PK || !companyFieldsExceptImageEqual(before, download.Company) {
		return nil, companyImageReference(current), false
	}
	expected := sha256.Sum256(source.Content)
	actual := sha256.Sum256(download.Content)
	state := &CompanyImageState{
		CompanyID: before.PK, HasImage: true, ImageURL: download.SourceURL, Filename: download.Filename,
		ContentType: metadata.ContentType, Size: int64(len(download.Content)), Width: metadata.Width, Height: metadata.Height,
		SHA256: hex.EncodeToString(actual[:]),
	}
	return state, state, expected == actual
}

func resolveCompanyImageSource(ctx context.Context, deps Dependencies, input SetCompanyImageInput) (upload.ResolvedSource, *mcp.CallToolResult, CompanyImageOutput, bool, error) {
	hasInline := strings.TrimSpace(input.InlineBase64) != ""
	hasLocal := strings.TrimSpace(input.LocalPath) != ""
	if hasInline == hasLocal {
		clarification := NewClarification("Which company image source should be used?", "upload_source", "set_company_image requires exactly one of inline_base64 or local_path", "inline_base64", true, nil, map[string]any{"company_id": input.CompanyID})
		return upload.ResolvedSource{}, TextResult(StatusClarificationRequired), CompanyImageOutput{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
	}
	if hasInline {
		raw := strings.TrimSpace(input.InlineBase64)
		if inlineBase64ExceedsMaxBytes(raw, companyImageMaxBytes(&deps)) {
			return upload.ResolvedSource{}, nil, CompanyImageOutput{}, false, errors.New("inline_base64 decoded content exceeds company image maximum bytes")
		}
		content, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return upload.ResolvedSource{}, nil, CompanyImageOutput{}, false, errors.New("inline_base64 is not valid base64")
		}
		source, err := upload.ResolveInline(ctx, upload.InlineSource{Content: content, Filename: input.Filename, ContentType: input.ContentType}, companyImageReadOptions(deps))
		return source, nil, CompanyImageOutput{}, err == nil, err
	}
	if isHTTPURLString(input.LocalPath) {
		clarification := NewClarification("Use the dedicated company image URL tool?", "upload_source", "set_company_image does not accept HTTP(S) URLs in local_path; use set_company_image_from_url", "url", true, nil, map[string]any{"company_id": input.CompanyID})
		return upload.ResolvedSource{}, TextResult(StatusClarificationRequired), CompanyImageOutput{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
	}
	source, err := upload.ResolveLocalFile(ctx, upload.LocalFileSource{Path: input.LocalPath, Filename: input.Filename, ContentType: input.ContentType}, upload.LocalFileOptions{
		ReadOptions: companyImageReadOptions(deps), Mode: deps.UploadMode, Fs: deps.UploadFS, AllowRoots: deps.UploadAllowRoots,
	})
	if recovery, ok := localUploadRecovery(err); ok {
		return upload.ResolvedSource{}, TextResult(StatusClarificationRequired), CompanyImageOutput{Status: StatusClarificationRequired, LocalUploadRecovery: recovery}, false, nil
	}
	return source, nil, CompanyImageOutput{}, err == nil, err
}

func companyImageReadOptions(deps Dependencies) upload.ReadOptions {
	options := uploadReadOptions(deps)
	options.MaxBytes = companyImageMaxBytes(&deps)
	return options
}

func companyImageMaxBytes(deps *Dependencies) int64 {
	if deps != nil && deps.UploadMaxBytes > 0 && deps.UploadMaxBytes < upload.CompanyImageMaxBytes {
		return deps.UploadMaxBytes
	}
	return upload.CompanyImageMaxBytes
}

func hasCompanyImage(company inventree.CompanyDetail) bool {
	return company.Image != nil && strings.TrimSpace(*company.Image) != ""
}

func companyImageReference(company inventree.CompanyDetail) *CompanyImageState {
	if company.PK <= 0 {
		return nil
	}
	state := &CompanyImageState{CompanyID: company.PK}
	if hasCompanyImage(company) {
		state.HasImage = true
		state.ImageURL = redactedMetadataURL(company.Image)
	}
	return state
}

func companyFieldsExceptImageEqual(before, after inventree.CompanyDetail) bool {
	before.Image = nil
	after.Image = nil
	return reflect.DeepEqual(before, after)
}

func companyImagePartial(before inventree.CompanyDetail, current *CompanyImageState, recovery string) (*mcp.CallToolResult, CompanyImageOutput, error) {
	if current == nil {
		current = companyImageReference(before)
	}
	return TextResult(StatusPartialFailure), CompanyImageOutput{Status: StatusPartialFailure, Current: current, RecoveryPlan: recovery}, nil
}

func exactCompanyImageTarget(ctx context.Context, client CompanyImageClient, id int) (inventree.CompanyDetail, error) {
	company, err := client.GetCompanyDetail(ctx, id)
	if err != nil {
		return inventree.CompanyDetail{}, safeCompanyAdminError("company image lookup", err)
	}
	if company.PK != id {
		return inventree.CompanyDetail{}, errors.New("company image lookup returned a mismatched identity")
	}
	return company, nil
}

func companyImageClarification(companyID int, question, reason, field string) (*mcp.CallToolResult, CompanyImageOutput, error) {
	clarification := NewClarification(question, field, reason, field, true, nil, map[string]any{"company_id": companyID})
	return TextResult(StatusClarificationRequired), CompanyImageOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}
