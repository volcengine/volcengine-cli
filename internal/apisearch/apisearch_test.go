package apisearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fixtures are real anonymous responses from OfficialEndpoint (2026-10-15):
// page0/page3 for Query=子网&Type=api&Limit=3 at Offset 0 and 3, and the
// HTTP 400 body for an empty Query.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func serve(t *testing.T, status int, body []byte, header map[string]string) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var requests []*http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Clone(r.Context()))
		for name, value := range header {
			w.Header().Set(name, value)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func TestProductionClientUsesTheOfficialEndpoint(t *testing.T) {
	client := New("ua")
	if client.endpoint != "https://api.volcengine.com/api/common/search/all-new" {
		t.Fatalf("endpoint = %q", client.endpoint)
	}
}

func TestSearchSendsAnonymousGETWithTheOfficialParameters(t *testing.T) {
	server, requests := serve(t, http.StatusOK, fixture(t, "page0.json"), nil)
	client := newClient(server.Client(), server.URL+"/api/common/search/all-new", "ve-test/1.0")
	if _, err := client.Search(context.Background(), SearchOptions{Query: "  子网 ", Limit: 3, Offset: 0}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	request := (*requests)[0]
	if request.Method != http.MethodGet || request.URL.Path != "/api/common/search/all-new" {
		t.Fatalf("request = %s %s", request.Method, request.URL)
	}
	query := request.URL.Query()
	if query.Get("Query") != "子网" || query.Get("Type") != "api" || query.Get("Limit") != "3" || query.Get("Offset") != "0" || len(query) != 4 {
		t.Fatalf("query parameters = %v", query)
	}
	if !strings.Contains(request.URL.RawQuery, "Query=%E5%AD%90%E7%BD%91") {
		t.Fatalf("Query is not URL-encoded: %s", request.URL.RawQuery)
	}
	// Anonymous: no credentials, signing, cookies or client identifiers.
	for _, header := range []string{"Authorization", "X-Security-Token", "X-Date", "Cookie", "X-Client-Id", "X-Volcengine-Credentials"} {
		if request.Header.Get(header) != "" {
			t.Fatalf("%s header was sent: %q", header, request.Header.Get(header))
		}
	}
	if request.Header.Get("User-Agent") != "ve-test/1.0" || request.Header.Get("Accept") != "application/json" {
		t.Fatalf("headers = %v", request.Header)
	}

	// Defaults: Limit 10, Offset 0.
	if _, err := client.Search(context.Background(), SearchOptions{Query: "subnet"}); err != nil {
		t.Fatal(err)
	}
	if query := (*requests)[1].URL.Query(); query.Get("Limit") != "10" || query.Get("Offset") != "0" {
		t.Fatalf("default parameters = %v", query)
	}
}

func TestSearchMapsTheRealResponse(t *testing.T) {
	server, _ := serve(t, http.StatusOK, fixture(t, "page0.json"), nil)
	result, err := newClient(server.Client(), server.URL, "").Search(context.Background(), SearchOptions{Query: "子网", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestID != "2026101516034111D59E449944A237509A" || result.Total != 200 || result.Limit != 3 || result.Offset != 0 || result.SkippedRecords != 0 {
		t.Fatalf("page = %+v", result)
	}
	if len(result.APIs) != 3 {
		t.Fatalf("records = %d", len(result.APIs))
	}
	first := result.APIs[0]
	want := APIRecord{
		ServiceCode: "vpc", ServiceName: "私有网络", Action: "CreateSubnet", Version: "2025-09-01",
		Title: "CreateSubnet", Summary: "创建子网", FullContent: "调用CreateSubnet，创建一个子网。",
		DocumentationURL: "https://api.volcengine.com/api-explorer?serviceCode=vpc&version=2025-09-01&action=CreateSubnet",
		Score:            first.Score,
	}
	if first != want || first.Score <= 0 {
		t.Fatalf("first record = %+v, want %+v", first, want)
	}
	if result.APIs[1].Action != "DescribeSubnets" || result.APIs[1].Summary != "查询子网列表" || strings.Contains(result.APIs[2].Summary, "<em>") {
		t.Fatalf("records = %+v", result.APIs)
	}
	if result.NextOffset == nil || *result.NextOffset != 3 {
		t.Fatalf("next offset = %v", result.NextOffset)
	}
}

func TestSearchPaginatesWithOffset(t *testing.T) {
	server, requests := serve(t, http.StatusOK, fixture(t, "page3.json"), nil)
	result, err := newClient(server.Client(), server.URL, "").Search(context.Background(), SearchOptions{Query: "子网", Limit: 3, Offset: 3})
	if err != nil {
		t.Fatal(err)
	}
	if (*requests)[0].URL.Query().Get("Offset") != "3" || result.Offset != 3 || result.NextOffset == nil || *result.NextOffset != 6 {
		t.Fatalf("page = %+v", result)
	}
	// Later pages include other versions of the same actions.
	if result.APIs[0].Action != "CreateSubnet" || result.APIs[0].Version != "2020-04-01" || result.APIs[1].ServiceCode != "aidap" {
		t.Fatalf("records = %+v", result.APIs)
	}

	last, _ := serve(t, http.StatusOK, []byte(`{"ResponseMetadata":{"RequestId":"r"},"Result":{"DataList":[{"Type":"api","BizInfo":{"ServiceCode":"vpc","Action":"A","Version":"v"}},{"Type":"api","BizInfo":{"ServiceCode":"vpc","Action":"B","Version":"v"}}],"Total":200,"Limit":3,"Offset":198}}`), nil)
	result, err = newClient(last.Client(), last.URL, "").Search(context.Background(), SearchOptions{Query: "x", Limit: 3, Offset: 198})
	if err != nil || result.NextOffset != nil || len(result.APIs) != 2 {
		t.Fatalf("last page = %+v, %v", result, err)
	}
	empty, _ := serve(t, http.StatusOK, []byte(`{"ResponseMetadata":{"RequestId":"r"},"Result":{"DataList":[],"Total":200,"Limit":3,"Offset":300}}`), nil)
	result, err = newClient(empty.Client(), empty.URL, "").Search(context.Background(), SearchOptions{Query: "x", Limit: 3, Offset: 300})
	if err != nil || result.NextOffset != nil || len(result.APIs) != 0 || result.APIs == nil {
		t.Fatalf("empty page = %+v, %v", result, err)
	}
}

func TestSearchSkipsRowsWithoutAnAPIIdentity(t *testing.T) {
	server, _ := serve(t, http.StatusOK, []byte(`{"ResponseMetadata":{"RequestId":"r"},"Result":{"DataList":[
 {"Type":"doc","Title":"guide"},
 {"Type":"api","Title":"no identity"},
 {"Type":"api","BizInfo":{"ServiceCode":"","Action":"X"}},
 {"Type":"api","BizInfo":{"ServiceCode":"ecs","Action":"DescribeInstances","Version":"2020-04-01"}}
],"Total":4,"Limit":4,"Offset":0}}`), nil)
	result, err := newClient(server.Client(), server.URL, "").Search(context.Background(), SearchOptions{Query: "x", Limit: 4})
	if err != nil || len(result.APIs) != 1 || result.SkippedRecords != 3 || result.APIs[0].Action != "DescribeInstances" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestSearchReportsUpstreamErrors(t *testing.T) {
	// The real HTTP 400 envelope for an invalid parameter.
	server, _ := serve(t, http.StatusBadRequest, fixture(t, "error_invalid_query.json"), nil)
	_, err := newClient(server.Client(), server.URL, "").Search(context.Background(), SearchOptions{Query: "x"})
	var upstream *UpstreamError
	if !errors.As(err, &upstream) || upstream.StatusCode != 400 || upstream.Code != "InvalidParam" || upstream.RequestID != "20261015160546A0CFF4EBC8181D4B6C80" || !strings.Contains(err.Error(), "expr_path=Query") {
		t.Fatalf("error = %#v", err)
	}

	gateway, _ := serve(t, http.StatusBadGateway, []byte(`<html>bad gateway</html>`), map[string]string{"X-Tt-Logid": "log-1"})
	_, err = newClient(gateway.Client(), gateway.URL, "").Search(context.Background(), SearchOptions{Query: "x"})
	if !errors.As(err, &upstream) || upstream.StatusCode != 502 || upstream.RequestID != "log-1" || !strings.Contains(err.Error(), "bad gateway") {
		t.Fatalf("gateway error = %#v", err)
	}

	for name, body := range map[string]string{"not json": `not json`, "missing result": `{"ResponseMetadata":{"RequestId":"r"}}`} {
		broken, _ := serve(t, http.StatusOK, []byte(body), nil)
		if _, err := newClient(broken.Client(), broken.URL, "").Search(context.Background(), SearchOptions{Query: "x"}); err == nil || !strings.Contains(err.Error(), "protocol error") {
			t.Fatalf("%s: error = %v", name, err)
		}
	}
}

func TestSearchValidatesOptionsBeforeSending(t *testing.T) {
	server, requests := serve(t, http.StatusOK, fixture(t, "page0.json"), nil)
	client := newClient(server.Client(), server.URL, "")
	for _, options := range []SearchOptions{
		{Query: " "}, {Query: "x", Limit: MaxLimit + 1}, {Query: "x", Limit: -1}, {Query: "x", Offset: -1},
		{Query: strings.Repeat("x", MaxQueryLength+1)},
	} {
		if _, err := client.Search(context.Background(), options); err == nil {
			t.Fatalf("options %+v were accepted", options)
		}
	}
	if len(*requests) != 0 {
		t.Fatalf("invalid options reached the endpoint %d times", len(*requests))
	}
}

func TestSearchHonorsContextAndTransportFailures(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := newClient(server.Client(), server.URL, "").Search(ctx, SearchOptions{Query: "x"}); err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("cancelled search error = %v", err)
	}
	if _, err := newClient(http.DefaultClient, "http://127.0.0.1:1/search", "").Search(context.Background(), SearchOptions{Query: "x"}); err == nil {
		t.Fatal("expected a transport error")
	}
}
