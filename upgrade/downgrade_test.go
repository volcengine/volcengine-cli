package upgrade

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type downgradeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f downgradeRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoUpgrade_DoesNotDowngradeToResolvedLatest(t *testing.T) {
	oldDownloadBase := os.Getenv(EnvDownloadBaseURL)
	os.Setenv(EnvDownloadBaseURL, "http://127.0.0.1:1")
	defer os.Setenv(EnvDownloadBaseURL, oldDownloadBase)

	SetHTTPClient(&http.Client{Transport: downgradeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		status := http.StatusInternalServerError
		body := "unavailable"
		if strings.HasSuffix(req.URL.Path, "/version_manifest.json") {
			status = http.StatusOK
			body = `{"latest":"1.0.0"}`
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})})
	defer SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout})

	var stdout bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "2.0.0",
		Yes:            true,
		Stdout:         &stdout,
	})
	if err != nil {
		t.Fatalf("default upgrade must not attempt a downgrade: %v", err)
	}
	if !strings.Contains(stdout.String(), "newer than the latest available version") {
		t.Fatalf("expected a no-downgrade explanation, got %q", stdout.String())
	}
}
