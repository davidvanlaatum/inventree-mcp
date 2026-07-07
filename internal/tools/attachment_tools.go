package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AttachmentWriteClient interface {
	GetPart(context.Context, int) (inventree.Part, error)
	ListAttachments(context.Context, inventree.AttachmentQuery) ([]inventree.Attachment, error)
	GetAttachmentMetadata(context.Context, int) (inventree.Attachment, error)
	DownloadAttachment(context.Context, int, inventree.AttachmentContentMode, int64) (inventree.DownloadedAttachment, error)
	UploadAttachment(context.Context, inventree.AttachmentCreate) (inventree.Attachment, error)
	CreateLinkAttachment(context.Context, inventree.AttachmentCreate) (inventree.Attachment, error)
	UpdateAttachmentMetadata(context.Context, int, inventree.PatchFields) (inventree.Attachment, error)
	DeleteAttachment(context.Context, int) error
	SetPartPrimaryImage(context.Context, int, inventree.PartPrimaryImageCreate) (inventree.Part, error)
}

type UploadAttachmentInput struct {
	ModelType      string   `json:"model_type" jsonschema:"In-scope InvenTree attachment target type."`
	ModelID        int      `json:"model_id" jsonschema:"Stable target object primary key."`
	Filename       string   `json:"filename,omitempty" jsonschema:"Attachment filename. Required for inline bytes; optional for local paths."`
	ContentType    string   `json:"content_type,omitempty" jsonschema:"Attachment content type."`
	InlineBase64   string   `json:"inline_base64,omitempty" jsonschema:"Base64-encoded upload content."`
	LocalPath      string   `json:"local_path,omitempty" jsonschema:"STDIO-only local path under a configured upload allowlist."`
	Comment        *string  `json:"comment,omitempty" jsonschema:"Optional attachment comment."`
	Tags           []string `json:"tags,omitempty" jsonschema:"Optional attachment tags."`
	AllowDuplicate bool     `json:"allow_duplicate,omitempty" jsonschema:"Set true to explicitly allow matching filename or size duplicates."`
}

type UploadAttachmentFromURLInput struct {
	ModelType      string   `json:"model_type" jsonschema:"In-scope InvenTree attachment target type."`
	ModelID        int      `json:"model_id" jsonschema:"Stable target object primary key."`
	URL            string   `json:"url" jsonschema:"HTTP(S) URL to fetch and upload as a file attachment."`
	Filename       string   `json:"filename,omitempty" jsonschema:"Optional filename override."`
	Comment        *string  `json:"comment,omitempty" jsonschema:"Optional attachment comment."`
	Tags           []string `json:"tags,omitempty" jsonschema:"Optional attachment tags."`
	AllowDuplicate bool     `json:"allow_duplicate,omitempty" jsonschema:"Set true to explicitly allow matching filename or size duplicates."`
}

type CreateLinkAttachmentInput struct {
	ModelType      string   `json:"model_type" jsonschema:"In-scope InvenTree attachment target type."`
	ModelID        int      `json:"model_id" jsonschema:"Stable target object primary key."`
	URL            string   `json:"url" jsonschema:"HTTP(S) URL to store as a link attachment without fetching."`
	Filename       string   `json:"filename,omitempty" jsonschema:"Optional filename for duplicate preflight. InvenTree assigns stored-link filename metadata."`
	Comment        *string  `json:"comment,omitempty" jsonschema:"Optional attachment comment."`
	Tags           []string `json:"tags,omitempty" jsonschema:"Optional attachment tags."`
	AllowDuplicate bool     `json:"allow_duplicate,omitempty" jsonschema:"Set true to explicitly allow matching filename or link duplicates."`
}

type UpdateAttachmentMetadataInput struct {
	ID       int      `json:"id" jsonschema:"Stable attachment primary key."`
	Filename *string  `json:"filename,omitempty" jsonschema:"Optional replacement filename."`
	Comment  *string  `json:"comment,omitempty" jsonschema:"Optional replacement comment."`
	Tags     []string `json:"tags,omitempty" jsonschema:"Optional replacement tags."`
}

type DeleteAttachmentInput struct {
	ID      int  `json:"id" jsonschema:"Stable attachment primary key."`
	Confirm bool `json:"confirm" jsonschema:"Required true before deleting an attachment."`
}

type SetPrimaryImageInput struct {
	PartID       int  `json:"part_id" jsonschema:"Stable part primary key."`
	AttachmentID int  `json:"attachment_id" jsonschema:"Stable image attachment primary key already attached to this part."`
	Confirm      bool `json:"confirm,omitempty" jsonschema:"Required true when replacing an existing primary image."`
}

type AttachmentWriteOutput struct {
	Status        string                 `json:"status"`
	Record        AttachmentMetadata     `json:"record,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
	SourceKind    string                 `json:"source_kind,omitempty"`
	PartID        int                    `json:"part_id,omitempty"`
	ImageURL      string                 `json:"image_url,omitempty"`
	Replaced      bool                   `json:"replaced,omitempty"`
}

func registerAttachmentWriteTools(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, UploadAttachmentToolName, "Upload attachment", "Uploads inline bytes or an allowlisted STDIO local file as an attachment.", uploadAttachment(deps))
	addWriteTool(server, UploadAttachmentFromURLToolName, "Upload attachment from URL", "Fetches an HTTP(S) URL under upload policy and uploads a copy as an attachment.", uploadAttachmentFromURL(deps))
	addWriteTool(server, CreateLinkAttachmentToolName, "Create link attachment", "Stores an HTTP(S) link attachment without fetching remote bytes.", createLinkAttachment(deps))
	addWriteTool(server, UpdateAttachmentMetadataToolName, "Update attachment metadata", "Partially updates attachment metadata fields.", updateAttachmentMetadata(deps))
	addWriteTool(server, DeleteAttachmentToolName, "Delete attachment", "Deletes one attachment after confirm:true.", deleteAttachment(deps))
	addWriteTool(server, SetPrimaryImageToolName, "Set primary image", "Sets a part primary image from an existing image attachment.", setPrimaryImage(deps))
}

func uploadAttachment(deps Dependencies) mcp.ToolHandlerFor[UploadAttachmentInput, AttachmentWriteOutput] {
	return LookupHandler[AttachmentWriteClient, UploadAttachmentInput, AttachmentWriteOutput](deps, UploadAttachmentToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AttachmentWriteClient, input UploadAttachmentInput) (*mcp.CallToolResult, AttachmentWriteOutput, error) {
			if result, output, ok := validateAttachmentTarget(input.ModelType, input.ModelID); !ok {
				return result, output, nil
			}
			source, result, output, ok, err := resolveUploadSource(ctx, deps, input)
			if err != nil || !ok {
				return result, output, err
			}
			if source.Filename == "" {
				return hardAttachmentClarification("Which filename should be used for this upload?", "filename", "upload_attachment requires a filename for the resolved source", "filename", map[string]any{"model_type": input.ModelType, "model_id": input.ModelID})
			}
			if strings.TrimSpace(source.ContentType) == "" {
				return hardAttachmentClarification("Which content type should be used for this upload?", "content_type", "upload_attachment requires content_type for inline and local file uploads", "content_type", map[string]any{"model_type": input.ModelType, "model_id": input.ModelID, "filename": source.Filename})
			}
			if result, output, ok, err := clarifyDuplicateAttachment(ctx, client, input.ModelType, input.ModelID, source.Filename, source.Size, "", input.AllowDuplicate); err != nil || !ok {
				return result, output, err
			}
			record, err := client.UploadAttachment(ctx, inventree.AttachmentCreate{
				ModelType:   input.ModelType,
				ModelID:     input.ModelID,
				Filename:    source.Filename,
				ContentType: source.ContentType,
				Content:     source.Content,
				Comment:     input.Comment,
				Tags:        input.Tags,
			})
			return attachmentWriteRecordOutput(record, string(source.Kind), err)
		})
}

func uploadAttachmentFromURL(deps Dependencies) mcp.ToolHandlerFor[UploadAttachmentFromURLInput, AttachmentWriteOutput] {
	return LookupHandler[AttachmentWriteClient, UploadAttachmentFromURLInput, AttachmentWriteOutput](deps, UploadAttachmentFromURLToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AttachmentWriteClient, input UploadAttachmentFromURLInput) (*mcp.CallToolResult, AttachmentWriteOutput, error) {
			if result, output, ok := validateAttachmentTarget(input.ModelType, input.ModelID); !ok {
				return result, output, nil
			}
			if filename := normalizedAttachmentFilename(input.Filename); filename != "" {
				if result, output, ok, err := clarifyDuplicateAttachment(ctx, client, input.ModelType, input.ModelID, filename, 0, "", input.AllowDuplicate); err != nil || !ok {
					return result, output, err
				}
			}
			fetcher := deps.URLFetcher
			source, err := fetcher.Fetch(ctx, upload.URLSource{URL: input.URL, Filename: input.Filename}, uploadReadOptions(deps))
			if err != nil {
				return nil, AttachmentWriteOutput{}, err
			}
			if source.Filename == "" {
				return hardAttachmentClarification("Which filename should be used for this URL upload?", "filename", "upload_attachment_from_url could not determine a filename", "filename", map[string]any{"model_type": input.ModelType, "model_id": input.ModelID})
			}
			if result, output, ok, err := clarifyDuplicateAttachment(ctx, client, input.ModelType, input.ModelID, source.Filename, source.Size, "", input.AllowDuplicate); err != nil || !ok {
				return result, output, err
			}
			record, err := client.UploadAttachment(ctx, inventree.AttachmentCreate{
				ModelType:   input.ModelType,
				ModelID:     input.ModelID,
				Filename:    source.Filename,
				ContentType: source.ContentType,
				Content:     source.Content,
				Comment:     input.Comment,
				Tags:        input.Tags,
			})
			return attachmentWriteRecordOutput(record, string(source.Kind), err)
		})
}

func createLinkAttachment(deps Dependencies) mcp.ToolHandlerFor[CreateLinkAttachmentInput, AttachmentWriteOutput] {
	return LookupHandler[AttachmentWriteClient, CreateLinkAttachmentInput, AttachmentWriteOutput](deps, CreateLinkAttachmentToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AttachmentWriteClient, input CreateLinkAttachmentInput) (*mcp.CallToolResult, AttachmentWriteOutput, error) {
			if result, output, ok := validateAttachmentTarget(input.ModelType, input.ModelID); !ok {
				return result, output, nil
			}
			linkURL, err := validateStoredLinkURL(input.URL)
			if err != nil {
				return nil, AttachmentWriteOutput{}, err
			}
			filename := normalizedAttachmentFilename(input.Filename)
			if result, output, ok, err := clarifyDuplicateAttachment(ctx, client, input.ModelType, input.ModelID, filename, 0, linkURL, input.AllowDuplicate); err != nil || !ok {
				return result, output, err
			}
			record, err := client.CreateLinkAttachment(ctx, inventree.AttachmentCreate{
				ModelType: input.ModelType,
				ModelID:   input.ModelID,
				Link:      linkURL,
				Comment:   input.Comment,
				Tags:      input.Tags,
			})
			return attachmentWriteRecordOutput(record, "link", err)
		})
}

func updateAttachmentMetadata(deps Dependencies) mcp.ToolHandlerFor[UpdateAttachmentMetadataInput, AttachmentWriteOutput] {
	return LookupHandler[AttachmentWriteClient, UpdateAttachmentMetadataInput, AttachmentWriteOutput](deps, UpdateAttachmentMetadataToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AttachmentWriteClient, input UpdateAttachmentMetadataInput) (*mcp.CallToolResult, AttachmentWriteOutput, error) {
			if input.ID <= 0 {
				return hardAttachmentClarification("Which attachment should be updated?", "attachment", "update_attachment_metadata requires a positive attachment id", "id", map[string]any{"id": input.ID})
			}
			existing, err := client.GetAttachmentMetadata(ctx, input.ID)
			if err != nil {
				return attachmentWriteRecordOutput(inventree.Attachment{}, "", err)
			}
			if err := validateAttachmentModelType(existing.ModelType); err != nil {
				return nil, AttachmentWriteOutput{}, err
			}
			fields := inventree.PatchFields{}
			if input.Filename != nil {
				fields["filename"] = inventree.Set(*input.Filename)
			}
			if input.Comment != nil {
				fields["comment"] = inventree.Set(*input.Comment)
			}
			if input.Tags != nil {
				fields["tags"] = inventree.Set(input.Tags)
			}
			if len(fields) == 0 {
				return hardAttachmentClarification("Which attachment metadata fields should be updated?", "attachment", "update_attachment_metadata requires at least one PATCH field", "id", map[string]any{"id": input.ID})
			}
			record, err := client.UpdateAttachmentMetadata(ctx, input.ID, fields)
			return attachmentWriteRecordOutput(record, "", err)
		})
}

func deleteAttachment(deps Dependencies) mcp.ToolHandlerFor[DeleteAttachmentInput, AttachmentWriteOutput] {
	return LookupHandler[AttachmentWriteClient, DeleteAttachmentInput, AttachmentWriteOutput](deps, DeleteAttachmentToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AttachmentWriteClient, input DeleteAttachmentInput) (*mcp.CallToolResult, AttachmentWriteOutput, error) {
			if input.ID <= 0 {
				return hardAttachmentClarification("Which attachment should be deleted?", "attachment", "delete_attachment requires a positive attachment id", "id", map[string]any{"id": input.ID})
			}
			existing, err := client.GetAttachmentMetadata(ctx, input.ID)
			if err != nil {
				return attachmentWriteRecordOutput(inventree.Attachment{}, "", err)
			}
			if err := validateAttachmentModelType(existing.ModelType); err != nil {
				return nil, AttachmentWriteOutput{}, err
			}
			if !input.Confirm {
				clarification := NewClarification("Confirm deletion of this attachment?", "confirm", "delete_attachment requires confirm:true before deleting", "confirm", true, []ClarificationCandidate{candidateFor(existing)}, map[string]any{"id": input.ID})
				return TextResult(StatusClarificationRequired), AttachmentWriteOutput{Status: StatusClarificationRequired, Record: sanitizeAttachment(existing), Clarification: &clarification}, nil
			}
			if err := client.DeleteAttachment(ctx, input.ID); err != nil {
				return nil, AttachmentWriteOutput{}, err
			}
			return TextResult(StatusOK), AttachmentWriteOutput{Status: StatusOK, Record: sanitizeAttachment(existing)}, nil
		})
}

func setPrimaryImage(deps Dependencies) mcp.ToolHandlerFor[SetPrimaryImageInput, AttachmentWriteOutput] {
	return LookupHandler[AttachmentWriteClient, SetPrimaryImageInput, AttachmentWriteOutput](deps, SetPrimaryImageToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client AttachmentWriteClient, input SetPrimaryImageInput) (*mcp.CallToolResult, AttachmentWriteOutput, error) {
			if input.PartID <= 0 {
				return hardAttachmentClarification("Which part should receive this primary image?", "part_id", "set_primary_image requires a positive part_id", "part_id", map[string]any{"part_id": input.PartID, "attachment_id": input.AttachmentID})
			}
			if input.AttachmentID <= 0 {
				return hardAttachmentClarification("Which image attachment should become primary?", "attachment_id", "set_primary_image requires a positive attachment_id", "attachment_id", map[string]any{"part_id": input.PartID, "attachment_id": input.AttachmentID})
			}
			part, err := client.GetPart(ctx, input.PartID)
			if err != nil {
				return attachmentWriteRecordOutput(inventree.Attachment{}, "", err)
			}
			attachment, err := client.GetAttachmentMetadata(ctx, input.AttachmentID)
			if err != nil {
				return attachmentWriteRecordOutput(inventree.Attachment{}, "", err)
			}
			if attachment.ModelType != "part" || attachment.ModelID != input.PartID {
				clarification := NewClarification("Which image attachment belongs to this part?", "attachment_id", "set_primary_image requires an image attachment attached to the requested part", "attachment_id", true, []ClarificationCandidate{candidateFor(attachment)}, map[string]any{"part_id": input.PartID, "attachment_id": input.AttachmentID})
				return TextResult(StatusClarificationRequired), AttachmentWriteOutput{Status: StatusClarificationRequired, Record: sanitizeAttachment(attachment), Clarification: &clarification, PartID: input.PartID}, nil
			}
			if !attachment.IsImage || attachment.Attachment == nil || strings.TrimSpace(*attachment.Attachment) == "" {
				clarification := NewClarification("Which image attachment should become primary?", "attachment_id", "set_primary_image requires an existing file attachment marked as an image", "attachment_id", true, []ClarificationCandidate{candidateFor(attachment)}, map[string]any{"part_id": input.PartID, "attachment_id": input.AttachmentID})
				return TextResult(StatusClarificationRequired), AttachmentWriteOutput{Status: StatusClarificationRequired, Record: sanitizeAttachment(attachment), Clarification: &clarification, PartID: input.PartID}, nil
			}
			replacing := part.Image != nil && strings.TrimSpace(*part.Image) != ""
			if replacing && !input.Confirm {
				clarification := NewClarification("Replace the existing primary image for this part?", "confirm", "set_primary_image requires confirm:true before replacing an existing primary image", "confirm", true, []ClarificationCandidate{candidateFor(attachment)}, map[string]any{"part_id": input.PartID, "attachment_id": input.AttachmentID})
				return TextResult(StatusClarificationRequired), AttachmentWriteOutput{Status: StatusClarificationRequired, Record: sanitizeAttachment(attachment), Clarification: &clarification, PartID: input.PartID, Replaced: true}, nil
			}
			download, err := client.DownloadAttachment(ctx, input.AttachmentID, inventree.AttachmentContentOriginal, uploadMaxBytes(deps))
			if err != nil {
				return nil, AttachmentWriteOutput{}, err
			}
			updatedPart, err := client.SetPartPrimaryImage(ctx, input.PartID, inventree.PartPrimaryImageCreate{
				Filename:    attachment.Filename,
				ContentType: download.ContentType,
				Content:     download.Content,
			})
			if err != nil {
				return nil, AttachmentWriteOutput{}, err
			}
			return TextResult(StatusOK), AttachmentWriteOutput{Status: StatusOK, Record: sanitizeAttachment(attachment), PartID: input.PartID, ImageURL: redactedMetadataURL(updatedPart.Image), Replaced: replacing}, nil
		})
}

func resolveUploadSource(ctx context.Context, deps Dependencies, input UploadAttachmentInput) (upload.ResolvedSource, *mcp.CallToolResult, AttachmentWriteOutput, bool, error) {
	hasInline := strings.TrimSpace(input.InlineBase64) != ""
	hasLocal := strings.TrimSpace(input.LocalPath) != ""
	if hasInline == hasLocal {
		clarification := NewClarification("Which upload source should be used?", "upload_source", "upload_attachment requires exactly one of inline_base64 or local_path", "inline_base64", true, nil, map[string]any{"model_type": input.ModelType, "model_id": input.ModelID})
		return upload.ResolvedSource{}, TextResult(StatusClarificationRequired), AttachmentWriteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
	}
	if hasInline {
		raw := strings.TrimSpace(input.InlineBase64)
		if inlineBase64ExceedsMaxBytes(raw, uploadMaxBytes(deps)) {
			return upload.ResolvedSource{}, nil, AttachmentWriteOutput{}, false, errors.New("inline_base64 decoded content exceeds upload max bytes")
		}
		content, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return upload.ResolvedSource{}, nil, AttachmentWriteOutput{}, false, errors.New("inline_base64 is not valid base64")
		}
		source, err := upload.ResolveInline(ctx, upload.InlineSource{Content: content, Filename: input.Filename, ContentType: input.ContentType}, uploadReadOptions(deps))
		return source, nil, AttachmentWriteOutput{}, err == nil, err
	}
	if isHTTPURLString(input.LocalPath) {
		clarification := NewClarification("Should this URL be uploaded as a copy or stored as a link?", "upload_source", "upload_attachment does not accept HTTP(S) URLs in local_path; use upload_attachment_from_url for a fetched copy or create_link_attachment for a stored link", "url", true, nil, map[string]any{"model_type": input.ModelType, "model_id": input.ModelID})
		return upload.ResolvedSource{}, TextResult(StatusClarificationRequired), AttachmentWriteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
	}
	source, err := upload.ResolveLocalFile(ctx, upload.LocalFileSource{Path: input.LocalPath, Filename: input.Filename, ContentType: input.ContentType}, upload.LocalFileOptions{
		ReadOptions: uploadReadOptions(deps),
		Mode:        deps.UploadMode,
		Fs:          deps.UploadFS,
		AllowRoots:  deps.UploadAllowRoots,
	})
	return source, nil, AttachmentWriteOutput{}, err == nil, err
}

func uploadReadOptions(deps Dependencies) upload.ReadOptions {
	timeout := deps.UploadTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return upload.ReadOptions{MaxBytes: deps.UploadMaxBytes, Timeout: timeout}
}

func uploadMaxBytes(deps Dependencies) int64 {
	if deps.UploadMaxBytes > 0 {
		return deps.UploadMaxBytes
	}
	return upload.DefaultMaxBytes
}

func inlineBase64ExceedsMaxBytes(raw string, maxBytes int64) bool {
	if maxBytes <= 0 {
		maxBytes = upload.DefaultMaxBytes
	}
	decodedLen := int64(base64.StdEncoding.DecodedLen(len(raw)))
	if len(raw)%4 == 0 {
		if strings.HasSuffix(raw, "==") {
			decodedLen -= 2
		} else if strings.HasSuffix(raw, "=") {
			decodedLen--
		}
	}
	return decodedLen > maxBytes
}

func isHTTPURLString(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func normalizedAttachmentFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return path.Base(strings.ReplaceAll(value, "\\", "/"))
}

func validateAttachmentTarget(modelType string, modelID int) (*mcp.CallToolResult, AttachmentWriteOutput, bool) {
	if err := validateAttachmentModelType(modelType); err != nil {
		clarification := NewClarification("Which in-scope object type should receive this attachment?", "model_type", err.Error(), "model_type", true, nil, map[string]any{"model_type": modelType, "model_id": modelID})
		return TextResult(StatusClarificationRequired), AttachmentWriteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, false
	}
	if modelID <= 0 {
		clarification := NewClarification("Which target object should receive this attachment?", "model_id", "attachment tools require a positive model_id", "model_id", true, nil, map[string]any{"model_type": modelType, "model_id": modelID})
		return TextResult(StatusClarificationRequired), AttachmentWriteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, false
	}
	return nil, AttachmentWriteOutput{}, true
}

func clarifyDuplicateAttachment(ctx context.Context, client AttachmentWriteClient, modelType string, modelID int, filename string, size int64, link string, allowDuplicate bool) (*mcp.CallToolResult, AttachmentWriteOutput, bool, error) {
	if allowDuplicate {
		return nil, AttachmentWriteOutput{}, true, nil
	}
	records, err := client.ListAttachments(ctx, inventree.AttachmentQuery{ModelType: modelType, ModelID: modelID, Limit: MaxLookupLimit})
	if err != nil {
		return nil, AttachmentWriteOutput{}, false, err
	}
	var candidates []ClarificationCandidate
	for _, record := range records {
		if attachmentDuplicates(record, filename, size, link) {
			candidates = append(candidates, candidateFor(record))
		}
	}
	if len(candidates) == 0 {
		return nil, AttachmentWriteOutput{}, true, nil
	}
	clarification := NewClarification("Should this duplicate attachment be added anyway?", "attachment", "matching attachment records already exist for the target object", "allow_duplicate", false, candidates, map[string]any{"model_type": modelType, "model_id": modelID, "filename": filename})
	return TextResult(StatusClarificationRequired), AttachmentWriteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, false, nil
}

func attachmentDuplicates(record inventree.Attachment, filename string, size int64, link string) bool {
	if filename != "" && record.Filename == filename {
		return true
	}
	if size > 0 && record.FileSize != nil && *record.FileSize == size {
		return true
	}
	if link != "" && record.Link != nil && *record.Link == link {
		return true
	}
	return false
}

func validateStoredLinkURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("parse link attachment URL failed")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("link attachment URL scheme must be http or https")
	}
	if parsed.Hostname() == "" {
		return "", errors.New("link attachment URL host is required")
	}
	if parsed.User != nil {
		return "", errors.New("link attachment URL must not include userinfo")
	}
	if parsed.Fragment != "" {
		return "", errors.New("link attachment URL must not include a fragment")
	}
	return parsed.String(), nil
}

func hardAttachmentClarification(question string, field string, reason string, retry string, retryValues map[string]any) (*mcp.CallToolResult, AttachmentWriteOutput, error) {
	clarification := NewClarification(question, field, reason, retry, true, nil, retryValues)
	return TextResult(StatusClarificationRequired), AttachmentWriteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func attachmentWriteRecordOutput(record inventree.Attachment, sourceKind string, err error) (*mcp.CallToolResult, AttachmentWriteOutput, error) {
	if err != nil {
		if isNotFound(err) {
			return TextResult(StatusNotFound), AttachmentWriteOutput{Status: StatusNotFound}, nil
		}
		return nil, AttachmentWriteOutput{}, err
	}
	return TextResult(StatusOK), AttachmentWriteOutput{Status: StatusOK, Record: sanitizeAttachment(record), SourceKind: sourceKind}, nil
}
