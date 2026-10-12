package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volcengine/volcengine-cli/internal/safety"
)

// Exercise the public CLI route with the PEM value format already covered by
// TestParserAcceptsPEMValuesStartingWithHyphens. No cloud endpoint is contacted.
func TestReviewSafetyApprovalPreservesPEMParameters(t *testing.T) {
	const service, action = "alb", "UploadCertificate"
	const publicKey = "-----BEGIN CERTIFICATE-----\ncertificate-data\n-----END CERTIFICATE-----"
	const privateKey = "-----BEGIN RSA PRIVATE KEY-----\nprivate-key-data\n-----END RSA PRIVATE KEY-----"
	if !rootSupport.IsValidAction(service, action) {
		t.Fatal("reproduction requires the bundled alb UploadCertificate action")
	}
	for _, name := range []string{"PublicKey", "PrivateKey", "CertificateType"} {
		if _, ok := publicActionParameterNames(service, action)[name]; !ok {
			t.Fatalf("bundled action does not expose %s", name)
		}
	}

	dir := reviewSafetyEnvironment(t)
	withIsolatedInvocationFlags(t)
	t.Setenv(envCLIDebug, "false")
	requests := make(chan map[string]interface{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := make(map[string]interface{})
		if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
				t.Errorf("decode local request: %v", err)
			}
		} else {
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse local request: %v", err)
			}
			for key := range r.Form {
				values[key] = r.Form.Get(key)
			}
		}
		requests <- values
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"review-local"},"Result":{}}`)
	}))
	defer server.Close()

	previousContext, previousConfig := ctx, config
	previousInput, previousOutput := safetyPolicyInput, safetyPolicyOutput
	previousHook := executeInvocationHook
	t.Cleanup(func() {
		ctx, config = previousContext, previousConfig
		safetyPolicyInput, safetyPolicyOutput = previousInput, previousOutput
		executeInvocationHook = previousHook
	})
	config = &Configure{Current: "review", Profiles: map[string]*Profile{
		"review": {Name: "review", Mode: ModeAK, AccessKey: "fake-ak", SecretKey: "fake-sk", Region: "cn-beijing", Endpoint: server.URL},
	}}
	if err := WriteConfigToFile(config); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VOLCENGINE_CLI_CONFIG_FILE", filepath.Join(dir, ConfigFile))
	ctx = NewContext()
	ctx.SetConfig(config)
	safetyPolicyInput, safetyPolicyOutput = strings.NewReader(""), io.Discard
	executeInvocationHook = nil

	for _, tc := range []struct {
		name        string
		enabled     bool
		approval    string
		envApproval bool
		wantBlocked bool
	}{
		{name: "disabled_control"},
		{name: "unapproved_control", enabled: true, wantBlocked: true},
		{name: "approved_long_flag", enabled: true, approval: "--yes"},
		{name: "approved_short_flag", enabled: true, approval: "-y"},
		{name: "environment_approval_control", enabled: true, envApproval: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := safety.SavePolicy(dir, &safety.Policy{Enabled: tc.enabled, Rules: []safety.Rule{{Pattern: service + ":" + action, Action: safety.ActionConfirm}}}); err != nil {
				t.Fatal(err)
			}
			if tc.envApproval {
				t.Setenv(safety.EnvSkipConfirm, "true")
			}
			args := []string{service, action, "--CertificateName", "review-pem", "--CertificateType", "Server", "--PublicKey", publicKey, "--PrivateKey", privateKey, "--endpoint", server.URL, "--output", "off"}
			if tc.approval != "" {
				args = append(args, tc.approval)
			}
			err := reviewRunAPICommand(t, args...)
			count := len(requests)
			t.Logf("approval=%q envApproval=%v error=%v HTTP requests=%d", tc.approval, tc.envApproval, err, count)
			if tc.wantBlocked {
				if err == nil || !strings.Contains(err.Error(), "requires confirmation") || count != 0 {
					t.Fatalf("want confirmation error with zero requests")
				}
				return
			}
			if err != nil || count != 1 {
				t.Fatalf("want successful invocation preserving PEM values and one local HTTP request")
			}
			values := <-requests
			if values["PublicKey"] != publicKey || values["PrivateKey"] != privateKey {
				t.Fatal("PEM values changed in the HTTP request")
			}
		})
	}
}
