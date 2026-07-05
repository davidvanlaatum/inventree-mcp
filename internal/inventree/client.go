package inventree

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	baseURL    *url.URL
	credential Credential
	httpClient *http.Client
}

type Config struct {
	BaseURL    string
	Credential Credential
	HTTPClient *http.Client
}

func NewClient(cfg Config) (*Client, error) {
	var validationErrors []error

	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		validationErrors = append(validationErrors, errors.New("InvenTree base URL must be an absolute URL"))
	} else if parsed.Scheme != "http" && parsed.Scheme != "https" {
		validationErrors = append(validationErrors, errors.New("InvenTree base URL scheme must be http or https"))
	}
	if err := cfg.Credential.Validate(); err != nil {
		validationErrors = append(validationErrors, err)
	}
	if err := errors.Join(validationErrors...); err != nil {
		return nil, err
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		baseURL:    normalizedBaseURL(parsed),
		credential: cfg.Credential,
		httpClient: httpClient,
	}, nil
}

func (c *Client) NewRequest(ctx context.Context, method string, path string, query url.Values, body any) (*http.Request, error) {
	target, err := c.resolve(path, query)
	if err != nil {
		return nil, err
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.credential.Apply(req)
	return req, nil
}

func (c *Client) DoJSON(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("InvenTree request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return parseAPIError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return fmt.Errorf("discard InvenTree response body: %w", err)
		}
		return nil
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode InvenTree response: %w", err)
	}
	return nil
}

func (c *Client) Patch(ctx context.Context, path string, fields PatchFields, out any) error {
	if len(fields) == 0 {
		return errors.New("InvenTree PATCH requires at least one field")
	}
	req, err := c.NewRequest(ctx, http.MethodPatch, path, nil, fields)
	if err != nil {
		return err
	}
	return c.DoJSON(req, out)
}

func (c *Client) Post(ctx context.Context, path string, body any, out any) error {
	req, err := c.NewRequest(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return err
	}
	return c.DoJSON(req, out)
}

func (c *Client) resolve(path string, query url.Values) (*url.URL, error) {
	if path == "" || !strings.HasPrefix(path, "/") {
		return nil, errors.New("InvenTree API path must start with /")
	}
	if strings.HasPrefix(path, "//") {
		return nil, errors.New("InvenTree API path must not be protocol-relative")
	}
	relative := &url.URL{Path: path}
	target := c.baseURL.ResolveReference(relative)
	if query != nil {
		target.RawQuery = query.Encode()
	}
	return target, nil
}

func normalizedBaseURL(value *url.URL) *url.URL {
	copy := *value
	copy.RawQuery = ""
	copy.Fragment = ""
	copy.Path = strings.TrimRight(copy.Path, "/") + "/"
	return &copy
}
