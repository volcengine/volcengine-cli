// Package docs implements unsigned access to the public Volcengine
// documentation search service.
package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/volcengine/volcengine-cli/internal/clientid"
	"github.com/volcengine/volcengine-cli/util"
	"golang.org/x/net/html"
)

const (
	apiBase              = "https://docs-api.cn-beijing.volces.com/api/v1/doc"
	requestTimeout       = 15 * time.Second
	DefaultSearchLimit   = 10
	DefaultSnippetLength = 600
	DefaultFetchLength   = 5000
)

var excessBlankLines = regexp.MustCompile(`\n[ \t]*\n[ \t]*(?:\n[ \t]*)+`)

// Module is the public documentation search interface used by the command
// adapter. Authentication, profiles and regions are deliberately absent.
type Module interface {
	Search(context.Context, SearchOptions) (SearchResult, error)
	Fetch(context.Context, FetchOptions) (FetchResult, error)
}

// SearchOptions controls one documentation search.
type SearchOptions struct {
	Query         string
	Limit         int
	ServiceCodes  []string
	SnippetLength int
}

// SearchDocument is one normalized search result.
type SearchDocument struct {
	Title        string   `json:"title"`
	URL          string   `json:"url"`
	ServiceCodes []string `json:"service_codes"`
	Snippet      string   `json:"snippet"`
}

// SearchResult is the stable structured output of Search.
type SearchResult struct {
	RequestID string           `json:"request_id"`
	Documents []SearchDocument `json:"documents"`
}

// FetchOptions controls local pagination after a document has been fetched.
type FetchOptions struct {
	URL        string
	StartIndex int
	MaxLength  int
}

// FetchResult is the stable structured output of Fetch.
type FetchResult struct {
	RequestID           string `json:"request_id"`
	Title               string `json:"title"`
	URL                 string `json:"url"`
	Content             string `json:"content"`
	StartIndex          int    `json:"start_index"`
	NextStartIndex      *int   `json:"next_start_index"`
	RemainingCharacters int    `json:"remaining_characters"`
	HasMore             bool   `json:"has_more"`
	HasContent          bool   `json:"has_content"`
}

// UpstreamError reports a business error returned in an HTTP 200 response.
type UpstreamError struct {
	Code      string
	Message   string
	RequestID string
}

func (e *UpstreamError) Error() string {
	detail := strings.TrimSpace(e.Code)
	if message := strings.TrimSpace(e.Message); message != "" {
		if detail != "" {
			detail += ": "
		}
		detail += message
	}
	if detail == "" {
		detail = "unknown error"
	}
	if e.RequestID != "" {
		return fmt.Sprintf("documentation service error: %s (RequestId: %s)", detail, e.RequestID)
	}
	return "documentation service error: " + detail
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type module struct {
	client    httpDoer
	baseURL   string
	userAgent string
	clientID  func() (string, error)
}

// New creates a production documentation module. The client uses the standard
// Go transport (including its proxy environment support), but never signs a
// request or loads CLI credentials. The local client ID is loaded lazily, only
// when a validated search/fetch request is about to be sent.
func New(userAgent string) Module {
	return newModule(&http.Client{Timeout: requestTimeout}, apiBase, userAgent, func() (string, error) {
		directory, err := util.GetConfigFileDir()
		if err != nil {
			return "", err
		}
		return clientid.LoadOrCreate(directory)
	})
}

func newModule(client httpDoer, baseURL, userAgent string, clientID func() (string, error)) *module {
	return &module{
		client:    client,
		baseURL:   strings.TrimRight(baseURL, "/"),
		userAgent: strings.TrimSpace(userAgent),
		clientID:  clientID,
	}
}

type responseMetadata struct {
	RequestID string         `json:"RequestId"`
	Error     *responseError `json:"Error"`
}

type responseError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

type searchResponse struct {
	ResponseMetadata *responseMetadata `json:"ResponseMetadata"`
	Result           *struct {
		DocList []searchResponseDocument `json:"DocList"`
	} `json:"Result"`
}

type searchResponseDocument struct {
	Title        string   `json:"Title"`
	URL          string   `json:"Url"`
	ServiceCodes []string `json:"ServiceCodes"`
	Content      string   `json:"Content"`
}

type fetchResponse struct {
	ResponseMetadata *responseMetadata `json:"ResponseMetadata"`
	Result           *struct {
		Title   string `json:"Title"`
		Content string `json:"Content"`
	} `json:"Result"`
}

// Search searches public Volcengine documentation and normalizes each hit.
func (m *module) Search(ctx context.Context, options SearchOptions) (SearchResult, error) {
	query := strings.TrimSpace(options.Query)
	if query == "" {
		return SearchResult{}, fmt.Errorf("search text must not be empty")
	}
	if options.Limit <= 0 {
		return SearchResult{}, fmt.Errorf("limit must be a positive integer")
	}
	if options.SnippetLength <= 0 {
		return SearchResult{}, fmt.Errorf("snippet-length must be a positive integer")
	}

	payload := struct {
		Query        string   `json:"Query"`
		Limit        int      `json:"Limit"`
		ServiceCodes []string `json:"ServiceCodes,omitempty"`
	}{
		Query:        query,
		Limit:        options.Limit,
		ServiceCodes: normalizeServiceCodes(options.ServiceCodes),
	}
	var response searchResponse
	if err := m.post(ctx, "search", payload, &response); err != nil {
		return SearchResult{}, err
	}
	if err := validateEnvelope(response.ResponseMetadata, response.Result != nil); err != nil {
		return SearchResult{}, err
	}

	result := SearchResult{
		RequestID: response.ResponseMetadata.RequestID,
		Documents: make([]SearchDocument, 0, len(response.Result.DocList)),
	}
	for _, document := range response.Result.DocList {
		title, content := decodeSearchContent(document.Content)
		if strings.TrimSpace(document.Title) != "" {
			title = document.Title
		}
		result.Documents = append(result.Documents, SearchDocument{
			Title:        title,
			URL:          document.URL,
			ServiceCodes: nonNilStrings(document.ServiceCodes),
			Snippet:      truncateRunes(cleanHTML(content), options.SnippetLength),
		})
	}
	return result, nil
}

// Fetch retrieves and locally paginates one public documentation article.
func (m *module) Fetch(ctx context.Context, options FetchOptions) (FetchResult, error) {
	cleanURL, err := normalizeDocumentURL(options.URL)
	if err != nil {
		return FetchResult{}, err
	}
	if options.StartIndex < 0 {
		return FetchResult{}, fmt.Errorf("start-index must be greater than or equal to zero")
	}
	if options.MaxLength <= 0 {
		return FetchResult{}, fmt.Errorf("max-length must be a positive integer")
	}

	payload := struct {
		URL string `json:"Url"`
	}{URL: cleanURL}
	var response fetchResponse
	if err := m.post(ctx, "fetch", payload, &response); err != nil {
		return FetchResult{}, err
	}
	if err := validateEnvelope(response.ResponseMetadata, response.Result != nil); err != nil {
		return FetchResult{}, err
	}

	content := cleanHTML(response.Result.Content)
	result := FetchResult{
		RequestID:  response.ResponseMetadata.RequestID,
		Title:      response.Result.Title,
		URL:        cleanURL,
		StartIndex: options.StartIndex,
		HasContent: content != "",
	}
	if content == "" {
		return result, nil
	}

	runes := []rune(content)
	if options.StartIndex >= len(runes) {
		return result, nil
	}
	end := options.StartIndex + options.MaxLength
	if end > len(runes) {
		end = len(runes)
	}
	result.Content = string(runes[options.StartIndex:end])
	result.RemainingCharacters = len(runes) - end
	result.HasMore = result.RemainingCharacters > 0
	if result.HasMore {
		next := end
		result.NextStartIndex = &next
	}
	return result, nil
}

func (m *module) post(ctx context.Context, path string, payload, response interface{}) error {
	if m == nil || m.client == nil {
		return fmt.Errorf("documentation HTTP client is not configured")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode documentation request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/"+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create documentation request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if m.userAgent != "" {
		request.Header.Set("User-Agent", m.userAgent)
	}
	if m.clientID == nil {
		return fmt.Errorf("documentation client ID provider is not configured")
	}
	id, err := m.clientID()
	if err != nil {
		return fmt.Errorf("load documentation client ID: %w", err)
	}
	request.Header.Set("X-Client-Id", id)

	httpResponse, err := m.client.Do(request)
	if err != nil {
		return fmt.Errorf("documentation request failed: %w", err)
	}
	if httpResponse == nil || httpResponse.Body == nil {
		return fmt.Errorf("documentation protocol error: empty HTTP response")
	}
	defer httpResponse.Body.Close()
	raw, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return fmt.Errorf("read documentation response: %w", err)
	}
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("documentation request failed with HTTP %d: %s", httpResponse.StatusCode, truncateRunes(strings.TrimSpace(string(raw)), 200))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("documentation protocol error: empty response body")
	}
	if err := json.Unmarshal(raw, response); err != nil {
		return fmt.Errorf("documentation protocol error: response is not valid JSON: %w", err)
	}
	return nil
}

func validateEnvelope(metadata *responseMetadata, hasResult bool) error {
	if metadata == nil {
		return fmt.Errorf("documentation protocol error: ResponseMetadata is missing")
	}
	if metadata.Error != nil {
		return &UpstreamError{
			Code:      metadata.Error.Code,
			Message:   metadata.Error.Message,
			RequestID: metadata.RequestID,
		}
	}
	if !hasResult {
		return fmt.Errorf("documentation protocol error: Result is missing")
	}
	return nil
}

func normalizeServiceCodes(codes []string) []string {
	result := make([]string, 0, len(codes))
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		result = append(result, code)
	}
	return result
}

func decodeSearchContent(raw string) (title, content string) {
	var inner struct {
		Title   string `json:"Title"`
		Content string `json:"Content"`
	}
	if err := json.Unmarshal([]byte(raw), &inner); err != nil {
		return "", raw
	}
	return inner.Title, inner.Content
}

func normalizeDocumentURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid document URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil || parsed.Port() != "" || (host != "www.volcengine.com" && host != "docs.volcengine.com") {
		return "", fmt.Errorf("document URL must use https://www.volcengine.com/docs/... or https://docs.volcengine.com/docs/...")
	}
	if !strings.HasPrefix(parsed.EscapedPath(), "/docs/") || strings.TrimPrefix(parsed.EscapedPath(), "/docs/") == "" {
		return "", fmt.Errorf("document URL must contain a /docs/... path")
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	parsed.Scheme = "https"
	parsed.Host = host
	return parsed.String(), nil
}

func cleanHTML(input string) string {
	if input == "" {
		return ""
	}
	document, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return strings.TrimSpace(input)
	}
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	cleaned := strings.ReplaceAll(text.String(), "\r\n", "\n")
	cleaned = strings.ReplaceAll(cleaned, "\r", "\n")
	cleaned = excessBlankLines.ReplaceAllString(cleaned, "\n\n")
	return strings.TrimSpace(cleaned)
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
