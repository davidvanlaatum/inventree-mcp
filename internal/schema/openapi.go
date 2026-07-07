package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type OpenAPI struct {
	OpenAPI    string                          `yaml:"openapi"`
	Info       Info                            `yaml:"info"`
	Paths      map[string]map[string]Operation `yaml:"paths"`
	Components Components                      `yaml:"components"`
}

type Info struct {
	Title   string `yaml:"title"`
	Version string `yaml:"version"`
}

type Components struct {
	Schemas map[string]SchemaRef `yaml:"schemas"`
}

type Operation struct {
	OperationID string              `yaml:"operationId"`
	Parameters  []Parameter         `yaml:"parameters"`
	RequestBody *RequestBody        `yaml:"requestBody"`
	Responses   map[string]Response `yaml:"responses"`
}

type Parameter struct {
	Name string `yaml:"name"`
	In   string `yaml:"in"`
}

type RequestBody struct {
	Content map[string]MediaType `yaml:"content"`
}

type Response struct {
	Content map[string]MediaType `yaml:"content"`
}

type MediaType struct {
	Schema SchemaRef `yaml:"schema"`
}

type SchemaRef struct {
	Ref        string               `yaml:"$ref"`
	Type       string               `yaml:"type"`
	Items      *SchemaRef           `yaml:"items"`
	Properties map[string]SchemaRef `yaml:"properties"`
	AllOf      []SchemaRef          `yaml:"allOf"`
}

func LoadOpenAPI(path string) (*OpenAPI, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read OpenAPI schema: %w", err)
	}
	doc, err := ParseOpenAPI(data)
	if err != nil {
		return nil, nil, err
	}
	return doc, data, nil
}

func ParseOpenAPI(data []byte) (*OpenAPI, error) {
	var doc OpenAPI
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse OpenAPI schema: %w", err)
	}
	if len(doc.Paths) == 0 {
		return nil, errors.New("OpenAPI schema has no paths")
	}
	return &doc, nil
}

func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (o *OpenAPI) Operation(path string, method string) (*Operation, bool) {
	methods, ok := o.Paths[path]
	if !ok {
		return nil, false
	}
	op, ok := methods[strings.ToLower(method)]
	if !ok {
		return nil, false
	}
	return &op, true
}

func (o *OpenAPI) HasSchemaRef(ref string) bool {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return false
	}
	_, ok := o.Components.Schemas[strings.TrimPrefix(ref, prefix)]
	return ok
}

func (op *Operation) RequestSchema(contentType string) (string, bool) {
	if op.RequestBody == nil {
		return "", false
	}
	media, ok := op.RequestBody.Content[contentType]
	if !ok {
		return "", false
	}
	return media.Schema.Reference(), true
}

func (op *Operation) HasRequestBody() bool {
	return op.RequestBody != nil && len(op.RequestBody.Content) > 0
}

func (op *Operation) HasQueryParameter(name string) bool {
	for _, parameter := range op.Parameters {
		if parameter.In == "query" && parameter.Name == name {
			return true
		}
	}
	return false
}

func (op *Operation) ResponseSchema(status string) (string, bool) {
	response, ok := op.Responses[status]
	if !ok {
		return "", false
	}
	media, ok := response.Content["application/json"]
	if !ok {
		return "", true
	}
	return media.Schema.Reference(), true
}

func (s SchemaRef) Reference() string {
	if s.Ref != "" {
		return s.Ref
	}
	if len(s.AllOf) > 0 {
		refs := make([]string, 0, len(s.AllOf))
		for _, entry := range s.AllOf {
			if ref := entry.Reference(); ref != "" {
				refs = append(refs, ref)
			}
		}
		return strings.Join(refs, ",")
	}
	if s.Type == "array" && s.Items != nil {
		return s.Items.Reference()
	}
	if result, ok := s.Properties["results"]; ok && result.Items != nil {
		return result.Items.Reference()
	}
	return ""
}

func (o *OpenAPI) ContainsForbiddenManifestPath(paths []string) string {
	for _, path := range paths {
		if _, exists := o.Paths[path]; exists {
			return path
		}
	}
	return ""
}

func ValidMethods() []string {
	return []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
}

func IsValidMethod(method string) bool {
	return slices.Contains(ValidMethods(), method)
}
