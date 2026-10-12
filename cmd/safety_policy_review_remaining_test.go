package cmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/volcengine/volcengine-cli/internal/safety"
)

// The agreed configuration contract treats enabled:null as disabled.
func TestReviewSafetyNullEnabledMeansDisabled(t *testing.T) {
	dir := reviewSafetyEnvironment(t)
	for _, name := range []string{"VOLCENGINE_PROFILE", "VOLCSTACK_PROFILE", "VOLCENGINE_ENDPOINT_RESOLVER"} {
		t.Cleanup(unsetenvForTest(t, name))
	}
	t.Setenv("VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS", "true")
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"review-local"},"Result":{}}`)
	}))
	defer server.Close()
	t.Setenv("VOLCENGINE_ENDPOINT", server.URL)
	invoker, err := newVolcengineMCPInvoker(&Configure{Profiles: map[string]*Profile{}}, "", "cn-beijing")
	if err != nil {
		t.Fatal(err)
	}
	registrar := &recordingMCPRegistrar{}
	registerMCPTools(registrar, rootSupport, invoker, &fakeDocsModule{})
	callContext := context.WithValue(context.Background(), mcpRequestMetadataContextKey{}, mcpRequestMetadata{
		Credentials: &mcpCredentials{AccessKeyID: "fake-ak", SecretAccessKey: "fake-sk"},
	})
	for _, tc := range []struct {
		name         string
		enabled      string
		wantError    bool
		wantRequests int32
	}{
		{name: "enabled_deny", enabled: "true", wantError: true},
		{name: "explicitly_disabled", enabled: "false", wantRequests: 1},
		{name: "null_disabled", enabled: "null", wantRequests: 1},
		{name: "invalid_string", enabled: `"invalid"`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := fmt.Sprintf(`{"enabled":%s,"rules":[{"pattern":"sts:*","action":"deny"}]}`, tc.enabled)
			if err := os.WriteFile(safety.PolicyFilePath(dir), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			atomic.StoreInt32(&requests, 0)
			result, err := registrar.handlers["call_api"](callContext, &protocol.CallToolRequest{Arguments: map[string]interface{}{
				"service": "sts", "api": "GetCallerIdentity",
			}})
			if err != nil || result == nil {
				t.Fatalf("call_api transport error: %v, result=%#v", err, result)
			}
			count := atomic.LoadInt32(&requests)
			t.Logf("enabled=%s toolError=%v HTTP requests=%d", tc.enabled, result.IsError, count)
			if result.IsError != tc.wantError || count != tc.wantRequests {
				t.Fatalf("want toolError=%v and HTTP requests=%d", tc.wantError, tc.wantRequests)
			}
		})
	}
}

// This helper runs the production configuration command in a separate process.
// Only the storage directory and start signal are supplied by the parent test;
// policy loading, mutation and saving have no stubs or injected delays.
func TestReviewSafetyPolicyAddProcess(t *testing.T) {
	dir := os.Getenv("VE_REVIEW_POLICY_DIRECTORY")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	configFileDirFunc = func() (string, error) { return dir, nil }
	command := newConfigureSafetyPolicyCommand()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"add", "--pattern", os.Getenv("VE_REVIEW_POLICY_PATTERN"), "--action", "deny"})
	fmt.Fprintln(os.Stdout, "ready")
	var signal [1]byte
	if _, err := io.ReadFull(os.Stdin, signal[:]); err != nil {
		t.Fatal(err)
	}
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestReviewSafetyConcurrentAddsPreserveBothRules(t *testing.T) {
	// Concurrent configuration updates were explicitly deferred during review.
	// Retain the reproducer as an opt-in check instead of a required regression.
	if os.Getenv("VE_REVIEW_CONCURRENT_POLICY_ADDS") != "1" {
		t.Skip("set VE_REVIEW_CONCURRENT_POLICY_ADDS=1 to run the deferred review reproducer")
	}
	dir := reviewSafetyEnvironment(t)
	patterns := []string{"ecs:Delete*", "sts:GetCallerIdentity"}
	for _, pattern := range patterns {
		if _, err := runSafetyPolicyCommandForTest(t, "add", "--pattern", pattern, "--action", "deny"); err != nil {
			t.Fatal(err)
		}
	}
	control, err := safety.LoadPolicy(dir)
	if err != nil || len(control.Rules) != 2 {
		t.Fatalf("sequential control: policy=%#v error=%v", control, err)
	}
	t.Log("sequential control: 2 successful commands, 2 rules retained")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	// A handful of real process races is enough to check this without changing
	// production scheduling. If all attempts preserve both rules, this test passes.
	for attempt := 1; attempt <= 5; attempt++ {
		if err := safety.SavePolicy(dir, &safety.Policy{Enabled: true, Rules: []safety.Rule{}}); err != nil {
			t.Fatal(err)
		}
		deadline, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		t.Cleanup(cancel)
		type child struct {
			command *exec.Cmd
			input   io.WriteCloser
			stderr  bytes.Buffer
		}
		children := make([]child, len(patterns))
		for i, pattern := range patterns {
			c := &children[i]
			c.command = exec.CommandContext(deadline, executable, "-test.run=^TestReviewSafetyPolicyAddProcess$")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "VE_REVIEW_POLICY_") {
					c.command.Env = append(c.command.Env, entry)
				}
			}
			c.command.Env = append(c.command.Env, "VE_REVIEW_POLICY_DIRECTORY="+dir, "VE_REVIEW_POLICY_PATTERN="+pattern)
			c.command.Stderr = &c.stderr
			c.input, err = c.command.StdinPipe()
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			output, err := c.command.StdoutPipe()
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			if err := c.command.Start(); err != nil {
				cancel()
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if c.command.ProcessState == nil {
					_ = c.input.Close()
					_ = c.command.Process.Kill()
					_ = c.command.Wait()
				}
			})
			line, err := bufio.NewReader(output).ReadString('\n')
			if err != nil || line != "ready\n" {
				cancel()
				_ = c.command.Wait()
				t.Fatalf("child not ready: line=%q error=%v stderr=%s", line, err, c.stderr.String())
			}
		}
		for i := range children {
			if _, err := children[i].input.Write([]byte{1}); err != nil {
				cancel()
				t.Fatal(err)
			}
			_ = children[i].input.Close()
		}
		successful := 0
		for i := range children {
			c := &children[i]
			if err := c.command.Wait(); err != nil {
				t.Errorf("add %s failed: %v; stderr=%s", patterns[i], err, c.stderr.String())
			} else {
				successful++
			}
		}
		cancel()
		if successful != len(patterns) {
			t.Fatal("could not verify two successful concurrent additions")
		}
		policy, err := safety.LoadPolicy(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("attempt=%d successful commands=%d retained rules=%v", attempt, successful, policy.Rules)
		for _, pattern := range patterns {
			found := false
			for _, rule := range policy.Rules {
				found = found || (rule.Pattern == pattern && rule.Action == safety.ActionDeny)
			}
			if !found {
				t.Fatalf("successful add lost its rule: %s", pattern)
			}
		}
	}
}
