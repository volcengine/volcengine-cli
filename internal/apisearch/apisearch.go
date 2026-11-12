// Package apisearch calls the official, public Volcengine API search endpoint
// used by the search_api MCP tool:
//
//	GET https://api.volcengine.com/api/common/search/all-new?Query=...&Type=api&Limit=10&Offset=0
//
// Requests are anonymous: no cloud credentials, signing, cookies or client ID.
// Results are search records (identity, title, summary, documentation URL),
// not full API definitions.
package apisearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// OfficialEndpoint is the public Volcengine search endpoint.
	OfficialEndpoint = "https://api.volcengine.com/api/common/search/all-new"

	DefaultLimit = 10
	// MaxLimit is the largest page size the endpoint returns (observed: a
	// larger Limit is answered with 50 records).
	MaxLimit       = 50
	MaxQueryLength = 1000

	recordType       = "api"
	requestTimeout   = 15 * time.Second
	maxResponseBytes = 4 << 20
)

// SearchOptions is one search page request.
type SearchOptions struct {
	Query  string
	Limit  int
	Offset int
}

// APIRecord is one API search record.
type APIRecord struct {
	ServiceCode      string  `json:"ServiceCode"`
	ServiceName      string  `json:"ServiceName,omitempty"`
	Action           string  `json:"Action"`
	Version          string  `json:"Version"`
	Title            string  `json:"Title,omitempty"`
	Summary          string  `json:"Summary,omitempty"`
	FullContent      string  `json:"FullContent,omitempty"`
	DocumentationURL string  `json:"DocumentationURL,omitempty"`
	Score            float64 `json:"Score"`
}

// SearchResult is one page of results.
type SearchResult struct {
	RequestID string      `json:"RequestId,omitempty"`
	Total     int         `json:"Total"`
	Limit     int         `json:"Limit"`
	Offset    int         `json:"Offset"`
	APIs      []APIRecord `json:"APIs"`
	// NextOffset is the Offset of the next page, or nil on the last page.
	NextOffset *int `json:"NextOffset,omitempty"`
	// SkippedRecords counts returned rows that were not API records with a
	// service code and action; they are not invented or repaired.
	SkippedRecords int `json:"SkippedRecords,omitempty"`
}

// UpstreamError is an error reported by the search endpoint.
type UpstreamError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
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
	if e.StatusCode != 0 && e.StatusCode != http.StatusOK {
		detail = fmt.Sprintf("HTTP %d: %s", e.StatusCode, detail)
	}
	if e.RequestID != "" {
		return fmt.Sprintf("API search service error: %s (RequestId: %s)", detail, e.RequestID)
	}
	return "API search service error: " + detail
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client calls the API search endpoint.
type Client struct {
	client    httpDoer
	endpoint  string
	userAgent string
}

// New returns a client for the official endpoint.
func New(userAgent string) *Client {
	return newClient(&http.Client{Timeout: requestTimeout}, OfficialEndpoint, userAgent)
}

func newClient(client httpDoer, endpoint, userAgent string) *Client {
	return &Client{client: client, endpoint: endpoint, userAgent: strings.TrimSpace(userAgent)}
}

type searchResponse struct {
	ResponseMetadata *struct {
		RequestID string `json:"RequestId"`
		Error     *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"ResponseMetadata"`
	Result *struct {
		DataList []struct {
			Type        string  `json:"Type"`
			Title       string  `json:"Title"`
			Abstract    string  `json:"Abstract"`
			FullContent string  `json:"FullContent"`
			URL         string  `json:"Url"`
			Score       float64 `json:"Score"`
			BizInfo     *struct {
				Action      string `json:"Action"`
				ServiceCn   string `json:"ServiceCn"`
				ServiceCode string `json:"ServiceCode"`
				Version     string `json:"Version"`
			} `json:"BizInfo"`
		} `json:"DataList"`
		Total  int `json:"Total"`
		Limit  int `json:"Limit"`
		Offset int `json:"Offset"`
	} `json:"Result"`
}

// Search requests one page of API records.
func (c *Client) Search(ctx context.Context, options SearchOptions) (SearchResult, error) {
	if c == nil || c.client == nil {
		return SearchResult{}, fmt.Errorf("API search client is not configured")
	}
	query := strings.TrimSpace(options.Query)
	if query == "" {
		return SearchResult{}, fmt.Errorf("query is required")
	}
	if utf8.RuneCountInString(query) > MaxQueryLength {
		return SearchResult{}, fmt.Errorf("query must be at most %d characters", MaxQueryLength)
	}
	limit := options.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return SearchResult{}, fmt.Errorf("limit must be between 1 and %d", MaxLimit)
	}
	if options.Offset < 0 {
		return SearchResult{}, fmt.Errorf("offset must be a non-negative integer")
	}

	values := url.Values{}
	values.Set("Query", query)
	values.Set("Type", recordType)
	values.Set("Limit", strconv.Itoa(limit))
	values.Set("Offset", strconv.Itoa(options.Offset))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return SearchResult{}, fmt.Errorf("create API search request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		request.Header.Set("User-Agent", c.userAgent)
	}

	response, err := c.client.Do(request)
	if err != nil {
		return SearchResult{}, fmt.Errorf("API search request failed: %w", err)
	}
	if response == nil || response.Body == nil {
		return SearchResult{}, fmt.Errorf("API search protocol error: empty HTTP response")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return SearchResult{}, fmt.Errorf("read API search response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return SearchResult{}, fmt.Errorf("API search protocol error: response exceeds %d bytes", maxResponseBytes)
	}

	var decoded searchResponse
	decodeErr := json.Unmarshal(raw, &decoded)
	requestID := strings.TrimSpace(response.Header.Get("X-Tt-Logid"))
	if decodeErr == nil && decoded.ResponseMetadata != nil {
		if id := strings.TrimSpace(decoded.ResponseMetadata.RequestID); id != "" {
			requestID = id
		}
		if failure := decoded.ResponseMetadata.Error; failure != nil {
			return SearchResult{}, &UpstreamError{StatusCode: response.StatusCode, Code: failure.Code, Message: failure.Message, RequestID: requestID}
		}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return SearchResult{}, &UpstreamError{StatusCode: response.StatusCode, Code: http.StatusText(response.StatusCode), Message: truncate(strings.TrimSpace(string(raw)), 200), RequestID: requestID}
	}
	if decodeErr != nil {
		return SearchResult{}, fmt.Errorf("API search protocol error: response is not valid JSON: %w", decodeErr)
	}
	if decoded.Result == nil {
		return SearchResult{}, fmt.Errorf("API search protocol error: Result is missing (RequestId: %s)", requestID)
	}

	result := SearchResult{RequestID: requestID, Total: decoded.Result.Total, Limit: decoded.Result.Limit, Offset: decoded.Result.Offset, APIs: []APIRecord{}}
	for _, row := range decoded.Result.DataList {
		info := row.BizInfo
		if row.Type != recordType || info == nil || strings.TrimSpace(info.ServiceCode) == "" || strings.TrimSpace(info.Action) == "" {
			result.SkippedRecords++
			continue
		}
		result.APIs = append(result.APIs, APIRecord{
			ServiceCode: strings.TrimSpace(info.ServiceCode), ServiceName: strings.TrimSpace(info.ServiceCn),
			Action: strings.TrimSpace(info.Action), Version: strings.TrimSpace(info.Version),
			Title: plainText(row.Title), Summary: plainText(row.Abstract), FullContent: strings.TrimSpace(row.FullContent),
			DocumentationURL: strings.TrimSpace(row.URL), Score: row.Score,
		})
	}
	if returned := len(decoded.Result.DataList); returned > 0 && result.Offset+returned < result.Total {
		next := result.Offset + returned
		result.NextOffset = &next
	}
	return result, nil
}

var highlightTags = regexp.MustCompile(`</?em>`)

// plainText removes the endpoint's <em> highlight markup.
func plainText(text string) string {
	return strings.TrimSpace(highlightTags.ReplaceAllString(text, ""))
}

func truncate(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "..."
}
