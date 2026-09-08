package docs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/volcengine/volcengine-cli/internal/clientid"
)

func TestNewUsesFixedEndpointTimeoutAndUserAgent(t *testing.T) {
	created, ok := New("  cli-user-agent  ").(*module)
	if !ok {
		t.Fatalf("New returned %T, want *module", New("ua"))
	}
	client, ok := created.client.(*http.Client)
	if !ok {
		t.Fatalf("client = %T, want *http.Client", created.client)
	}
	if client.Timeout != 15*time.Second || created.baseURL != apiBase || created.userAgent != "cli-user-agent" {
		t.Fatalf("module config: timeout=%s baseURL=%q userAgent=%q", client.Timeout, created.baseURL, created.userAgent)
	}
}

func TestSearchSendsExactPayloadAndNormalizesDocuments(t *testing.T) {
	var gotPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", request.Method)
		}
		if request.URL.Path != "/search" {
			t.Errorf("path = %s, want /search", request.URL.Path)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := request.Header.Get("User-Agent"); got != "test-user-agent" {
			t.Errorf("User-Agent = %q", got)
		}
		if got := request.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization must be absent, got %q", got)
		}
		if err := json.NewDecoder(request.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		io.WriteString(writer, `{
  "ResponseMetadata":{"RequestId":"req-search"},
  "Result":{"DocList":[
    {"Title":"外层标题","Url":"https://docs.volcengine.com/docs/1/2","ServiceCodes":["tos"],"Content":"{\"Title\":\"内层标题\",\"Content\":\"<b>甲&amp;乙</b>丙\"}"},
    {"Title":" ","Url":"https://www.volcengine.com/docs/3/4","Content":"{\"Title\":\"回退标题\",\"Content\":\"正文\\n\\n \\n\\n尾\"}"},
    {"Title":"原串","Url":"https://www.volcengine.com/docs/5/6","Content":"<i>原始&amp;值</i>"}
  ]}
}`)
	}))
	defer server.Close()

	module := newModule(server.Client(), server.URL, "test-user-agent", fixedClientID)
	result, err := module.Search(context.Background(), SearchOptions{
		Query:         "  对象存储  ",
		Limit:         3,
		ServiceCodes:  []string{" tos ", "", "ecs", "tos"},
		SnippetLength: 3,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	wantPayload := map[string]interface{}{
		"Query":        "对象存储",
		"Limit":        float64(3),
		"ServiceCodes": []interface{}{"tos", "ecs", "tos"},
	}
	if !reflect.DeepEqual(gotPayload, wantPayload) {
		t.Fatalf("payload = %#v, want %#v", gotPayload, wantPayload)
	}
	if result.RequestID != "req-search" || len(result.Documents) != 3 {
		t.Fatalf("result = %#v", result)
	}
	if got := result.Documents[0]; got.Title != "外层标题" || got.Snippet != "甲&乙" || !utf8.ValidString(got.Snippet) {
		t.Errorf("first document = %#v", got)
	}
	if got := result.Documents[1]; got.Title != "回退标题" || got.Snippet != "正文\n" {
		t.Errorf("second document = %#v", got)
	}
	if got := result.Documents[2]; got.Snippet != "原始&" {
		t.Errorf("invalid inner JSON fallback = %#v", got)
	}
	if result.Documents[1].ServiceCodes == nil {
		t.Error("structured service_codes must be an empty array, not null")
	}
}

func TestSearchOmitsEmptyServiceCodes(t *testing.T) {
	var rawBody string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		rawBody = string(body)
		io.WriteString(writer, `{"ResponseMetadata":{"RequestId":"r"},"Result":{"DocList":[]}}`)
	}))
	defer server.Close()

	result, err := newModule(server.Client(), server.URL, "ua", fixedClientID).Search(context.Background(), SearchOptions{
		Query: "query", Limit: 1, ServiceCodes: []string{" ", ""}, SnippetLength: 1,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if strings.Contains(rawBody, "ServiceCodes") {
		t.Fatalf("empty ServiceCodes must be omitted: %s", rawBody)
	}
	if result.Documents == nil || len(result.Documents) != 0 {
		t.Fatalf("documents = %#v, want non-nil empty slice", result.Documents)
	}
}

func TestFetchSendsOnlyCleanURLAndPaginatesUnicode(t *testing.T) {
	var gotPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/fetch" {
			t.Errorf("path = %s, want /fetch", request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		io.WriteString(writer, `{"ResponseMetadata":{"RequestId":"req-fetch"},"Result":{"Title":"文章","Content":"<b>甲乙</b>丙丁戊"}}`)
	}))
	defer server.Close()

	result, err := newModule(server.Client(), server.URL, "ua", fixedClientID).Fetch(context.Background(), FetchOptions{
		URL:        "https://www.volcengine.com/docs/6349/162514?lang=zh#part",
		StartIndex: 1,
		MaxLength:  2,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	wantPayload := map[string]interface{}{"Url": "https://www.volcengine.com/docs/6349/162514"}
	if !reflect.DeepEqual(gotPayload, wantPayload) {
		t.Fatalf("payload = %#v, want %#v", gotPayload, wantPayload)
	}
	if result.Content != "乙丙" || result.RemainingCharacters != 2 || !result.HasMore || result.NextStartIndex == nil || *result.NextStartIndex != 3 {
		t.Fatalf("pagination result = %#v", result)
	}
	if !result.HasContent || result.StartIndex != 1 || result.URL != wantPayload["Url"] {
		t.Fatalf("fetch metadata = %#v", result)
	}
}

func TestFetchCanonicalizesDocumentHostCase(t *testing.T) {
	var gotURL string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			URL string `json:"Url"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotURL = payload.URL
		io.WriteString(writer, `{"ResponseMetadata":{"RequestId":"r"},"Result":{"Title":"t","Content":"c"}}`)
	}))
	defer server.Close()

	_, err := newModule(server.Client(), server.URL, "ua", fixedClientID).Fetch(context.Background(), FetchOptions{
		URL: "HTTPS://WWW.VOLCENGINE.COM/docs/1/2?x=1#y", MaxLength: 1,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotURL != "https://www.volcengine.com/docs/1/2" {
		t.Fatalf("clean URL = %q", gotURL)
	}
}

func TestFetchPastEndAndEmptyArticleAreSuccessful(t *testing.T) {
	responses := []string{
		`{"ResponseMetadata":{"RequestId":"past"},"Result":{"Title":"短文","Content":"甲乙"}}`,
		`{"ResponseMetadata":{"RequestId":"empty"},"Result":{"Title":"","Content":"  "}}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		io.WriteString(writer, responses[0])
		responses = responses[1:]
	}))
	defer server.Close()
	module := newModule(server.Client(), server.URL, "ua", fixedClientID)

	past, err := module.Fetch(context.Background(), FetchOptions{URL: "https://docs.volcengine.com/docs/1/2", StartIndex: 9, MaxLength: 2})
	if err != nil {
		t.Fatalf("Fetch past end: %v", err)
	}
	if past.Content != "" || past.HasMore || past.NextStartIndex != nil || past.RemainingCharacters != 0 || !past.HasContent {
		t.Fatalf("past-end result = %#v", past)
	}
	empty, err := module.Fetch(context.Background(), FetchOptions{URL: "https://www.volcengine.com/docs/6349", MaxLength: 5})
	if err != nil {
		t.Fatalf("Fetch navigation page: %v", err)
	}
	if empty.HasContent || empty.HasMore || empty.NextStartIndex != nil {
		t.Fatalf("empty article = %#v", empty)
	}
}

func TestBusinessErrorPreservesCodeMessageAndRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		io.WriteString(writer, `{"ResponseMetadata":{"RequestId":"req-error","Error":{"Code":"InvalidRequest","Message":"query is required"}},"Result":{"DocList":[]}}`)
	}))
	defer server.Close()

	_, err := newModule(server.Client(), server.URL, "ua", fixedClientID).Search(context.Background(), SearchOptions{Query: "q", Limit: 1, SnippetLength: 1})
	var upstream *UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %v, want UpstreamError", err)
	}
	if upstream.Code != "InvalidRequest" || upstream.Message != "query is required" || upstream.RequestID != "req-error" {
		t.Fatalf("upstream error = %#v", upstream)
	}
	for _, expected := range []string{"InvalidRequest", "query is required", "req-error"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error %q missing %q", err, expected)
		}
	}
}

func TestTransportAndProtocolFailuresAreNotEmptyResults(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{name: "non 2xx", statusCode: http.StatusBadGateway, body: "upstream down", want: "HTTP 502"},
		{name: "empty", statusCode: http.StatusOK, body: " ", want: "empty response body"},
		{name: "invalid json", statusCode: http.StatusOK, body: "not-json", want: "not valid JSON"},
		{name: "missing metadata", statusCode: http.StatusOK, body: `{"Result":{"DocList":[]}}`, want: "ResponseMetadata is missing"},
		{name: "missing result", statusCode: http.StatusOK, body: `{"ResponseMetadata":{"RequestId":"r"}}`, want: "Result is missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.statusCode)
				io.WriteString(writer, test.body)
			}))
			defer server.Close()
			_, err := newModule(server.Client(), server.URL, "ua", fixedClientID).Search(context.Background(), SearchOptions{Query: "q", Limit: 1, SnippetLength: 1})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestNetworkFailurePreservesTransportCause(t *testing.T) {
	cause := errors.New("dial failed")
	module := newModule(doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	}), "https://example.com", "ua", fixedClientID)
	_, err := module.Search(context.Background(), SearchOptions{Query: "q", Limit: 1, SnippetLength: 1})
	if err == nil || !strings.Contains(err.Error(), "documentation request failed") {
		t.Fatalf("error = %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error %v does not preserve transport cause", err)
	}
}

func TestInputValidationHappensBeforeTransport(t *testing.T) {
	doer := &countingDoer{}
	idCalls := 0
	module := newModule(doer, "https://example.com", "ua", func() (string, error) {
		idCalls++
		return fixedClientID()
	})
	searchCases := []SearchOptions{
		{Query: " ", Limit: 1, SnippetLength: 1},
		{Query: "q", Limit: 0, SnippetLength: 1},
		{Query: "q", Limit: 1, SnippetLength: 0},
	}
	for _, options := range searchCases {
		if _, err := module.Search(context.Background(), options); err == nil {
			t.Errorf("Search(%#v) succeeded", options)
		}
	}
	invalidURLs := []string{
		"http://www.volcengine.com/docs/1/2",
		"https://example.com/docs/1/2",
		"https://www.volcengine.com/other/1/2",
		"https://www.volcengine.com/docs/",
		"not-a-url",
	}
	for _, rawURL := range invalidURLs {
		if _, err := module.Fetch(context.Background(), FetchOptions{URL: rawURL, MaxLength: 1}); err == nil {
			t.Errorf("Fetch(%q) succeeded", rawURL)
		}
	}
	for _, options := range []FetchOptions{
		{URL: "https://www.volcengine.com/docs/1/2", StartIndex: -1, MaxLength: 1},
		{URL: "https://www.volcengine.com/docs/1/2", StartIndex: 0, MaxLength: 0},
	} {
		if _, err := module.Fetch(context.Background(), options); err == nil {
			t.Errorf("Fetch(%#v) succeeded", options)
		}
	}
	if doer.calls != 0 {
		t.Fatalf("transport calls = %d, want 0", doer.calls)
	}
	if idCalls != 0 {
		t.Fatalf("invalid input loaded the client ID %d times", idCalls)
	}
}

func TestSearchAndFetchSharePersistedClientID(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "client")
	var ids []string
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, r.Header.Get("X-Client-Id"))
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") != "test-agent" {
			t.Error("client ID changed authentication or User-Agent")
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for key := range body {
			if strings.Contains(strings.ToLower(key), "client") {
				t.Errorf("client ID leaked into request payload as %q", key)
			}
		}
		io.WriteString(w, `{"ResponseMetadata":{"RequestId":"r"},"Result":{"DocList":[],"Title":"title","Content":"content"}}`)
	}))
	defer server.Close()
	loadID := func() (string, error) { return clientid.LoadOrCreate(directory) }
	searcher := newModule(server.Client(), server.URL, "test-agent", loadID)
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("module construction touched client ID storage: %v", err)
	}
	if _, err := searcher.Search(context.Background(), SearchOptions{Query: "q", Limit: 1, SnippetLength: 1}); err != nil {
		t.Fatal(err)
	}
	// A new module instance represents a later CLI invocation.
	fetcher := newModule(server.Client(), server.URL, "test-agent", loadID)
	if _, err := fetcher.Fetch(context.Background(), FetchOptions{URL: "https://www.volcengine.com/docs/1/2", MaxLength: 100}); err != nil {
		t.Fatal(err)
	}
	persisted, err := clientid.LoadOrCreate(directory)
	if err != nil || len(ids) != 2 || ids[0] != persisted || ids[1] != persisted {
		t.Fatalf("request IDs = %v, persisted = %q, err = %v", ids, persisted, err)
	}
	if !reflect.DeepEqual(paths, []string{"/search", "/fetch"}) {
		t.Fatalf("request paths = %v", paths)
	}
}

func TestClientIDFailurePreventsBothRequests(t *testing.T) {
	cause := errors.New("client ID storage is unavailable")
	doer := &countingDoer{}
	module := newModule(doer, "https://example.com", "ua", func() (string, error) { return "", cause })
	_, searchErr := module.Search(context.Background(), SearchOptions{Query: "q", Limit: 1, SnippetLength: 1})
	_, fetchErr := module.Fetch(context.Background(), FetchOptions{URL: "https://www.volcengine.com/docs/1/2", MaxLength: 1})
	for _, err := range []error{searchErr, fetchErr} {
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "client ID") {
			t.Errorf("client ID failure was not preserved: %v", err)
		}
	}
	if doer.calls != 0 {
		t.Fatalf("sent %d requests without a persistent ID", doer.calls)
	}
}

func TestCleanHTMLUsesParserDecodesEntitiesAndFoldsBlankLines(t *testing.T) {
	input := "  <div>&lt;标题&gt;&amp;</div>\r\n\r\n \r\n\t\r\n正文  "
	if got, want := cleanHTML(input), "<标题>&\n\n正文"; got != want {
		t.Fatalf("cleanHTML = %q, want %q", got, want)
	}
}

type countingDoer struct {
	calls int
}

func (d *countingDoer) Do(*http.Request) (*http.Response, error) {
	d.calls++
	return nil, errors.New("unexpected transport call")
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func fixedClientID() (string, error) {
	return "d284587e-0c76-4a60-80b9-f2d85de7d836", nil
}
