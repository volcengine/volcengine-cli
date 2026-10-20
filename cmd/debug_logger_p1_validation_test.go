package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/volcengine/volcengine-go-sdk/volcengine/client"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
)

func TestRedactSensitiveTextValidation(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		masks       int
	}{
		{"leading_quote", "SessionToken='<REDACTED_A>", 1},
		{"middle_quote", "SessionToken=<REDACTED_A>'<REDACTED_B>", 1},
		{"encoded_value", "SessionToken=%27%3CREDACTED_A%3E", 1},
		{"encoded_key", "Session%54oken='<REDACTED_A>", 1},
		{"mixed_case", "sEsSiOnToKeN='<REDACTED_A>&pAsSwOrD=<REDACTED_B>'", 2},
		{"repeated", "SessionToken='<REDACTED_A>&SessionToken=<REDACTED_B>'<REDACTED_C>", 2},
		{"quoted_key", "Session'Token=<REDACTED_A>", 1},
		{"short_keys", "AK='<REDACTED_A>&sK=<REDACTED_B>'&Pwd=<REDACTED_C>", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `Get "https://example.invalid/?` + tc.query + `&Action=Inspect&TaskId=public'Task&Version=v1": transport stopped`
			got := redactSensitiveText(raw)
			assertSafeQueryError(t, got)
			if strings.Count(got, maskedDebugValue) != tc.masks {
				t.Fatal("sensitive query occurrences were not all masked")
			}
			if !strings.Contains(got, "TaskId=public'Task") || !strings.Contains(got, "Version=v1") {
				t.Fatal("non-sensitive query values changed")
			}
			if redactSensitiveText(got) != got {
				t.Fatal("redaction is not idempotent")
			}
		})
	}
	t.Run("non_sensitive", func(t *testing.T) {
		input := "https://example.invalid/?Action=Inspect&TaskId=public'Task&DiskId=public&Version=v1"
		if redactSensitiveText(input) != input {
			t.Fatal("non-sensitive query must remain byte-identical")
		}
	})
	t.Run("outer_single_quotes", func(t *testing.T) {
		got := redactSensitiveText("Get 'https://example.invalid/?Action=Inspect&SessionToken='<REDACTED_A>': transport stopped")
		assertSafeQueryError(t, got)
	})
}

func assertSafeQueryError(t *testing.T, got string) {
	t.Helper()
	if strings.Contains(got, "REDACTED") || strings.Contains(got, "%52%45%44") {
		t.Fatal("error output exposed a sensitive fixture value")
	}
	if !strings.Contains(got, maskedDebugValue) || !strings.Contains(got, "Action=Inspect") || !strings.Contains(got, "transport stopped") {
		t.Fatal("redacted output lost masking or diagnostic context")
	}
}

type validationRoundTripper func(*http.Request) (*http.Response, error)

func (f validationRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// The client follows a real redirect, but every transport attempt stays in memory.
func TestRedirectErrorOutputValidation(t *testing.T) {
	for _, query := range []struct{ name, value string }{
		{"leading", "SessionToken='%3CREDACTED_A%3E"},
		{"middle", "SessionToken=%3CREDACTED_A%3E'%3CREDACTED_B%3E"},
		{"encoded_repeated", "Session%54oken=%27%3CREDACTED_A%3E&sEsSiOnToKeN=%3CREDACTED_B%3E"},
	} {
		t.Run(query.name, func(t *testing.T) {
			cause := errors.New("transport stopped")
			attempts := 0
			redirectURL := "https://example.invalid/redirect?Action=Inspect&" + query.value + "&TaskId=public"
			httpClient := &http.Client{Transport: validationRoundTripper(func(r *http.Request) (*http.Response, error) {
				attempts++
				if attempts == 1 {
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{redirectURL}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				if attempts != 2 || r.URL.Path != "/redirect" || r.URL.Query().Get("Action") != "Inspect" {
					t.Fatal("client did not follow the in-memory redirect")
				}
				return nil, cause
			})}
			_, err := httpClient.Get("https://example.invalid/start")
			var urlErr *url.Error
			if attempts != 2 || !errors.Is(err, cause) || !errors.As(err, &urlErr) || urlErr.URL != redirectURL {
				t.Fatal("fixture did not produce the expected redirected transport error")
			}
			wrapped := fmt.Errorf("request failed: %w", err)
			t.Run("formatActionError", func(t *testing.T) {
				got := formatActionError(wrapped)
				if got == nil {
					t.Fatal("action formatting discarded error")
				}
				assertSafeQueryError(t, got.Error())
				var recovered *url.Error
				if errors.Unwrap(got) != wrapped || !errors.Is(got, cause) || !errors.As(got, &recovered) || recovered != urlErr {
					t.Fatal("redaction did not preserve error identity and chain")
				}
			})
			for _, stage := range []string{"debugLogSdkEnd", "debugLogError", "sdk_handler"} {
				t.Run(stage, func(t *testing.T) {
					var output bytes.Buffer
					logger := &DebugLogger{enabled: true, out: &output}
					switch stage {
					case "debugLogSdkEnd":
						debugLogSdkEnd(logger, time.Now(), wrapped)
					case "debugLogError":
						debugLogError(logger, "validation", wrapped)
					case "sdk_handler":
						sdk := &SdkClient{DebugLogger: logger}
						c := &client.Client{}
						sdk.addDebugRequestAttemptHandler(c)
						r := &request.Request{Error: wrapped, Operation: &request.Operation{Name: "Inspect", HTTPMethod: http.MethodGet}}
						c.Handlers.CompleteAttempt.Run(r)
						if r.Error != wrapped || !errors.Is(r.Error, cause) {
							t.Fatal("SDK debug handler mutated request error")
						}
					}
					assertSafeQueryError(t, output.String())
					if !strings.Contains(output.String(), "TaskId=public") {
						t.Fatal("logging lost non-sensitive query context")
					}
				})
			}
		})
	}
	t.Run("plain_error_identity", func(t *testing.T) {
		cause := errors.New("transport stopped")
		if formatActionError(cause) != cause || formatActionError(nil) != nil {
			t.Fatal("plain error and nil identity must be preserved")
		}
	})
}
