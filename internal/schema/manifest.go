package schema

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	Schema               ManifestSchema       `yaml:"schema"`
	CoverageAreas        []string             `yaml:"coverage_areas"`
	ForbiddenPaths       []string             `yaml:"forbidden_paths"`
	AttachmentModelTypes AttachmentModelTypes `yaml:"attachment_model_types"`
	Endpoints            []Endpoint           `yaml:"endpoints"`
}

type AttachmentModelTypes struct {
	InScope  []string `yaml:"in_scope"`
	Deferred []string `yaml:"deferred"`
}

type ManifestSchema struct {
	Path       string `yaml:"path"`
	SHA256     string `yaml:"sha256"`
	OpenAPI    string `yaml:"openapi"`
	APIVersion string `yaml:"api_version"`
}

type Endpoint struct {
	ID                 string   `yaml:"id"`
	Area               string   `yaml:"area"`
	Path               string   `yaml:"path"`
	Method             string   `yaml:"method"`
	OperationID        string   `yaml:"operation_id"`
	RequiredQuery      []string `yaml:"required_query"`
	RequestSchema      string   `yaml:"request_schema"`
	RequestContentType string   `yaml:"request_content_type"`
	ResponseSchema     string   `yaml:"response_schema"`
	ResponseStatus     string   `yaml:"response_status"`
}

func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read endpoint manifest: %w", err)
	}
	var manifest Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse endpoint manifest: %w", err)
	}
	if len(manifest.Endpoints) == 0 {
		return nil, errors.New("endpoint manifest has no endpoints")
	}
	return &manifest, nil
}

func (m *Manifest) Validate(openapi *OpenAPI, schemaDigest string) error {
	var errs []error
	if m.Schema.Path != "docs/api-schema.yaml" {
		errs = append(errs, fmt.Errorf("manifest schema path %q must be docs/api-schema.yaml", m.Schema.Path))
	}
	if m.Schema.SHA256 != schemaDigest {
		errs = append(errs, fmt.Errorf("manifest schema sha256 %q does not match docs/api-schema.yaml sha256 %q", m.Schema.SHA256, schemaDigest))
	}
	if m.Schema.OpenAPI != openapi.OpenAPI {
		errs = append(errs, fmt.Errorf("manifest OpenAPI version %q does not match schema %q", m.Schema.OpenAPI, openapi.OpenAPI))
	}
	if m.Schema.APIVersion != openapi.Info.Version {
		errs = append(errs, fmt.Errorf("manifest API version %q does not match schema %q", m.Schema.APIVersion, openapi.Info.Version))
	}
	areas := map[string]bool{}
	for _, area := range m.CoverageAreas {
		areas[area] = false
	}
	if len(areas) == 0 {
		errs = append(errs, errors.New("manifest must declare coverage_areas"))
	}
	seenIDs := map[string]struct{}{}
	for _, endpoint := range m.Endpoints {
		if err := endpoint.Validate(openapi, areas); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", endpoint.ID, err))
		}
		if endpoint.ID == "" {
			continue
		}
		if _, duplicate := seenIDs[endpoint.ID]; duplicate {
			errs = append(errs, fmt.Errorf("%s: duplicate endpoint id", endpoint.ID))
		}
		seenIDs[endpoint.ID] = struct{}{}
	}
	for area, covered := range areas {
		if !covered {
			errs = append(errs, fmt.Errorf("coverage area %q has no endpoints", area))
		}
	}
	if forbidden := m.findForbiddenEndpoint(); forbidden != "" {
		errs = append(errs, fmt.Errorf("manifest includes deferred file-surface endpoint %s", forbidden))
	}
	if err := m.validateAttachmentModelTypes(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (e Endpoint) Validate(openapi *OpenAPI, areas map[string]bool) error {
	var errs []error
	if e.ID == "" {
		errs = append(errs, errors.New("endpoint id is required"))
	}
	if e.Area == "" {
		errs = append(errs, errors.New("area is required"))
	} else if _, ok := areas[e.Area]; !ok {
		errs = append(errs, fmt.Errorf("area %q is not listed in coverage_areas", e.Area))
	} else {
		areas[e.Area] = true
	}
	if e.Path == "" {
		errs = append(errs, errors.New("path is required"))
	}
	if !IsValidMethod(e.Method) {
		errs = append(errs, fmt.Errorf("method %q is not one of %s", e.Method, strings.Join(ValidMethods(), ", ")))
	}
	op, ok := openapi.Operation(e.Path, e.Method)
	if !ok {
		errs = append(errs, fmt.Errorf("schema does not define %s %s", e.Method, e.Path))
		return errors.Join(errs...)
	}
	if e.OperationID != op.OperationID {
		errs = append(errs, fmt.Errorf("operation_id %q does not match schema operationId %q", e.OperationID, op.OperationID))
	}
	for _, query := range e.RequiredQuery {
		if !op.HasQueryParameter(query) {
			errs = append(errs, fmt.Errorf("required query parameter %q is not defined by schema", query))
		}
	}
	if op.HasRequestBody() && e.RequestSchema == "" {
		errs = append(errs, errors.New("request_schema is required when schema operation has a request body"))
	}
	if e.RequestSchema != "" && e.RequestContentType == "" {
		errs = append(errs, errors.New("request_content_type is required when request_schema is set"))
	}
	if e.RequestContentType != "" {
		got, ok := op.RequestSchema(e.RequestContentType)
		if !ok {
			errs = append(errs, fmt.Errorf("schema does not define request content type %q", e.RequestContentType))
		} else if got != e.RequestSchema {
			errs = append(errs, fmt.Errorf("request_schema %q does not match schema %q", e.RequestSchema, got))
		}
	}
	if e.RequestSchema != "" && !openapi.HasSchemaRef(e.RequestSchema) {
		errs = append(errs, fmt.Errorf("request_schema %q does not exist in components.schemas", e.RequestSchema))
	}
	if e.ResponseStatus == "" {
		e.ResponseStatus = defaultResponseStatus(e.Method)
	}
	got, ok := op.ResponseSchema(e.ResponseStatus)
	if !ok {
		errs = append(errs, fmt.Errorf("schema does not define response status %q", e.ResponseStatus))
	} else if got != "" && e.ResponseSchema == "" {
		errs = append(errs, fmt.Errorf("response_schema is required for JSON response status %q", e.ResponseStatus))
	} else if e.ResponseSchema != "" && got != e.ResponseSchema {
		errs = append(errs, fmt.Errorf("response_schema %q does not match schema %q", e.ResponseSchema, got))
	}
	if e.ResponseSchema != "" && !openapi.HasSchemaRef(e.ResponseSchema) {
		errs = append(errs, fmt.Errorf("response_schema %q does not exist in components.schemas", e.ResponseSchema))
	}
	return errors.Join(errs...)
}

func (m *Manifest) validateAttachmentModelTypes() error {
	var errs []error
	seen := map[string]string{}
	for _, modelType := range m.AttachmentModelTypes.InScope {
		seen[modelType] = "in_scope"
	}
	for _, modelType := range m.AttachmentModelTypes.Deferred {
		if firstList := seen[modelType]; firstList != "" {
			errs = append(errs, fmt.Errorf("attachment model type %q appears in both %s and deferred", modelType, firstList))
		}
		seen[modelType] = "deferred"
	}
	if len(m.AttachmentModelTypes.InScope) == 0 {
		errs = append(errs, errors.New("attachment_model_types.in_scope must not be empty"))
	}
	if len(m.AttachmentModelTypes.Deferred) == 0 {
		errs = append(errs, errors.New("attachment_model_types.deferred must not be empty"))
	}
	return errors.Join(errs...)
}

func (m *Manifest) findForbiddenEndpoint() string {
	for _, endpoint := range m.Endpoints {
		for _, forbidden := range m.ForbiddenPaths {
			if endpoint.Path == forbidden {
				return endpoint.Path
			}
		}
	}
	return ""
}

func defaultResponseStatus(method string) string {
	switch method {
	case "POST":
		return "201"
	case "DELETE":
		return "204"
	default:
		return "200"
	}
}
