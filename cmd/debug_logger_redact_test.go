package cmd

import (
	"errors"
	"strings"
	"testing"
)

// SDK transport errors embed the full request URL; sensitive query values must
// be masked before the error reaches stderr or the debug log.
func TestRedactSensitiveTextMasksURLQueryValues(t *testing.T) {
	raw := `RequestError: send request failed caused by: Get "http://127.0.0.1:1/?Action=ListVersions&SessionToken=SECRET_1&sessiontoken=SECRET_2&Session%54oken=SECRET_3&Password=SECRET_4&SessionToken=SECRET_5&Version=2025-06-01": dial tcp 127.0.0.1:1: connect: connection refused`
	got := redactSensitiveText(raw)
	if strings.Contains(got, "SECRET_") {
		t.Fatalf("sensitive value leaked: %s", got)
	}
	for _, keep := range []string{"Action=ListVersions", "Version=2025-06-01", "SessionToken=" + maskedDebugValue, "connection refused"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("redacted text missing %q: %s", keep, got)
		}
	}
}

func TestFormatActionErrorRedactsAndKeepsCause(t *testing.T) {
	cause := errors.New(`Get "https://open.example.com/?Action=X&SessionToken=SECRET_VALUE": EOF`)
	err := formatActionError(cause)
	if strings.Contains(err.Error(), "SECRET_VALUE") {
		t.Fatalf("formatActionError leaked secret: %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("formatActionError should keep the original error chain")
	}

	plain := errors.New("InvalidParameter: Action=X")
	if got := formatActionError(plain); got != plain {
		t.Fatalf("errors without sensitive values should be returned unchanged, got %v", got)
	}
}
