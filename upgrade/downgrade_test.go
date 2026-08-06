package upgrade

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type downgradeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f downgradeRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoUpgrade_DoesNotDowngradeToResolvedLatest(t *testing.T) {
	forceStandaloneDetect(t)
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

	execPath := filepath.Join(t.TempDir(), BinaryName())
	if err := ioutil.WriteFile(execPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "2.0.0",
		Yes:            true,
		Stdout:         &stdout,
		ExecPath:       execPath,
	})
	if err != nil {
		t.Fatalf("default upgrade must not attempt a downgrade: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "newer than the latest available version") {
		t.Fatalf("expected a no-downgrade explanation, got %q", out)
	}
	if !strings.Contains(out, "no upgrade available") {
		t.Fatalf("expected no-upgrade wording, got %q", out)
	}
}

func TestDoUpgrade_RejectsExplicitDowngrade(t *testing.T) {
	forceStandaloneDetect(t)

	downloadHit := false
	SetHTTPClient(&http.Client{Transport: downgradeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		downloadHit = true
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("should not download")),
			Request:    req,
		}, nil
	})})
	defer SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout})

	execPath := filepath.Join(t.TempDir(), BinaryName())
	if err := ioutil.WriteFile(execPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "2.0.0",
		TargetVersion:  "1.0.0",
		Yes:            true,
		Stdout:         &stdout,
		ExecPath:       execPath,
	})
	if err == nil {
		t.Fatal("expected error for explicit downgrade")
	}
	msg := err.Error()
	if !strings.Contains(msg, "refusing to install older version") {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(msg, "1.0.0") || !strings.Contains(msg, "2.0.0") {
		t.Fatalf("error should include both versions: %v", err)
	}
	if downloadHit {
		t.Fatal("explicit downgrade must not download")
	}
}

func TestDoUpgrade_NPMRejectsExplicitDowngrade(t *testing.T) {
	forceNPMDetect(t)
	var calls [][]string
	stubExecRecording(t, &calls, true)

	var stdout bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "2.0.0",
		TargetVersion:  "1.0.0",
		Yes:            true,
		Stdout:         &stdout,
		ExecPath:       filepath.Join(t.TempDir(), "ve"),
	})
	if err == nil {
		t.Fatal("expected error for npm explicit downgrade")
	}
	if !strings.Contains(err.Error(), "refusing to install older version") {
		t.Fatalf("err: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("npm must not run when explicit downgrade is rejected: %#v", calls)
	}
	if strings.Contains(stdout.String(), "npm install") {
		t.Fatalf("npm command must not print on rejected downgrade:\n%s", stdout.String())
	}
}

func TestDoUpgrade_NPMRejectsInvalidVersionPin(t *testing.T) {
	forceNPMDetect(t)
	var calls [][]string
	stubExecRecording(t, &calls, true)

	var stdout bytes.Buffer
	err := DoUpgrade(Options{
		CurrentVersion: "2.0.0",
		TargetVersion:  "bad!!version",
		Yes:            true,
		Stdout:         &stdout,
		ExecPath:       filepath.Join(t.TempDir(), "ve"),
	})
	if err == nil {
		t.Fatal("expected invalid target version error for npm pin")
	}
	if !strings.Contains(err.Error(), "invalid target version") {
		t.Fatalf("err: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("npm must not run when version pin is invalid: %#v", calls)
	}
	if strings.Contains(stdout.String(), "npm install") {
		t.Fatalf("npm command must not print on invalid pin:\n%s", stdout.String())
	}
}

func TestDoUpgrade_RejectsIncomparableExplicitTarget(t *testing.T) {
	forceStandaloneDetect(t)

	downloadHit := false
	SetHTTPClient(&http.Client{Transport: downgradeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		downloadHit = true
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("should not download")),
			Request:    req,
		}, nil
	})})
	defer SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout})

	execPath := filepath.Join(t.TempDir(), BinaryName())
	if err := ioutil.WriteFile(execPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	err := DoUpgrade(Options{
		CurrentVersion: "1.0.50",
		TargetVersion:  "dev-build",
		Yes:            true,
		Stdout:         ioutil.Discard,
		ExecPath:       execPath,
	})
	if err == nil {
		t.Fatal("expected error for incomparable explicit target")
	}
	msg := err.Error()
	if !strings.Contains(msg, "must be newer") {
		t.Fatalf("expected not-newer wording, got: %v", err)
	}
	if strings.Contains(msg, "older version") {
		t.Fatalf("incomparable target must not claim older: %v", err)
	}
	if downloadHit {
		t.Fatal("incomparable target must not download")
	}
}

func TestErrTargetNotNewer_Wording(t *testing.T) {
	older := errTargetNotNewer("2.0.0", "1.0.0").Error()
	if !strings.Contains(older, "older version") {
		t.Fatalf("semver downgrade should say older: %s", older)
	}

	incomp := errTargetNotNewer("1.0.50", "dev-build").Error()
	if !strings.Contains(incomp, "must be newer") {
		t.Fatalf("incomparable should say not newer: %s", incomp)
	}
	if strings.Contains(incomp, "older version") {
		t.Fatalf("incomparable must not say older: %s", incomp)
	}
}

func TestRejectOlderTarget_InvalidAndOK(t *testing.T) {
	if err := rejectOlderTarget("1.0.0", ""); err != nil {
		t.Fatalf("empty pin should be allowed: %v", err)
	}
	if err := rejectOlderTarget("1.0.0", "2.0.0"); err != nil {
		t.Fatalf("newer pin should be allowed: %v", err)
	}
	if err := rejectOlderTarget("1.0.0", "1.0.0"); err != nil {
		t.Fatalf("same pin should be allowed: %v", err)
	}
	if err := rejectOlderTarget("2.0.0", "1.0.0"); err == nil {
		t.Fatal("older pin should be rejected")
	}
	if err := rejectOlderTarget("1.0.0", "../evil"); err == nil {
		t.Fatal("invalid pin should be rejected")
	} else if !strings.Contains(err.Error(), "invalid target version") {
		t.Fatalf("err: %v", err)
	}
}
