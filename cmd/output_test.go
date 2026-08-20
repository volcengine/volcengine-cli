package cmd

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/volcengine/volcengine-cli/util/output"
)

func withAPIOutputWriter(t *testing.T, w io.Writer) {
	t.Helper()
	prev := apiOutputWriter
	apiOutputWriter = w
	t.Cleanup(func() { apiOutputWriter = prev })
}

func TestResolveOutputFormatDefault(t *testing.T) {
	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	got, err := resolveOutputFormat(c)
	if err != nil {
		t.Fatal(err)
	}
	if got != output.FormatJSON {
		t.Fatalf("got %q, want json", got)
	}
}

func TestResolveOutputFormatValue(t *testing.T) {
	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	f, _ := c.fixedFlags.AddByName("output")
	f.SetValue("TABLE")
	got, err := resolveOutputFormat(c)
	if err != nil {
		t.Fatal(err)
	}
	if got != output.FormatTable {
		t.Fatalf("got %q, want table", got)
	}
}

func TestResolveOutputFormatInvalid(t *testing.T) {
	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	f, _ := c.fixedFlags.AddByName("output")
	f.SetValue("xml")
	if _, err := resolveOutputFormat(c); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveAPIOutputPlanValidatesQuery(t *testing.T) {
	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	qFlag, _ := c.fixedFlags.AddByName("query")
	qFlag.SetValue("[[[")
	if _, err := resolveAPIOutputPlan(c); err == nil || !strings.Contains(err.Error(), "--query") {
		t.Fatalf("expected --query validation error, got %v", err)
	}
}

func TestExecuteInvocationRejectsInvalidOutputBeforeClientSetup(t *testing.T) {
	c := NewContext()
	outFlag, _ := c.fixedFlags.AddByName("output")
	outFlag.SetValue("xml")
	buildCalled := false
	err := executeInvocation(c, invocationParams{
		serviceName: "ecs",
		action:      "DescribeInstances",
		version:     "2020-04-01",
		method:      "GET",
	}, func() (invocationInput, error) {
		buildCalled = true
		return invocationInput{value: map[string]interface{}{}}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported output format") {
		t.Fatalf("expected output validation error, got %v", err)
	}
	if buildCalled {
		t.Fatal("request input must not be built when output validation fails")
	}
}

func TestExecuteInvocationRejectsInvalidQueryWithOffBeforeClientSetup(t *testing.T) {
	c := NewContext()
	outFlag, _ := c.fixedFlags.AddByName("output")
	outFlag.SetValue("off")
	qFlag, _ := c.fixedFlags.AddByName("query")
	qFlag.SetValue("[[[")
	buildCalled := false
	err := executeInvocation(c, invocationParams{
		serviceName: "ecs",
		action:      "DescribeInstances",
		version:     "2020-04-01",
		method:      "GET",
	}, func() (invocationInput, error) {
		buildCalled = true
		return invocationInput{value: map[string]interface{}{}}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "--query") {
		t.Fatalf("expected query validation error, got %v", err)
	}
	if buildCalled {
		t.Fatal("request input must not be built when output is off and query is invalid")
	}
}

func TestExecuteInvocationRejectsInvalidQueryBeforeClientSetup(t *testing.T) {
	c := NewContext()
	qFlag, _ := c.fixedFlags.AddByName("query")
	qFlag.SetValue("[[[")
	buildCalled := false
	err := executeInvocation(c, invocationParams{
		serviceName: "ecs",
		action:      "DescribeInstances",
		version:     "2020-04-01",
		method:      "GET",
	}, func() (invocationInput, error) {
		buildCalled = true
		return invocationInput{value: map[string]interface{}{}}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "--query") {
		t.Fatalf("expected query validation error, got %v", err)
	}
	if buildCalled {
		t.Fatal("request input must not be built when query validation fails")
	}
}

func TestRenderAPIOutputQueryAndTable(t *testing.T) {
	var buf bytes.Buffer
	withAPIOutputWriter(t, &buf)

	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	outFlag, _ := c.fixedFlags.AddByName("output")
	outFlag.SetValue("table")
	qFlag, _ := c.fixedFlags.AddByName("query")
	qFlag.SetValue("Result.Instances[*].{Id:InstanceId,Status:Status}")

	data := map[string]interface{}{
		"Result": map[string]interface{}{
			"Instances": []interface{}{
				map[string]interface{}{"InstanceId": "i-1", "Status": "RUNNING"},
			},
		},
	}
	if err := renderAPIOutput(c, nil, data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "i-1") {
		t.Fatalf("table output unexpected:\n%s", buf.String())
	}
}

// The renderer strips ResponseMetadata only when no --query was given. These
// two cases must agree about whether the field exists: before Options.Queried
// was wired up, a bare table hid RequestId while an explicit query for it still
// printed a value.
func TestRenderAPIOutputMetadataVisibilityIsConsistent(t *testing.T) {
	data := func() map[string]interface{} {
		return map[string]interface{}{
			"ResponseMetadata": map[string]interface{}{"RequestId": "req-1"},
			"Result": map[string]interface{}{
				"Instances": []interface{}{
					map[string]interface{}{"InstanceId": "i-1"},
				},
			},
		}
	}

	// No --query: the envelope is display noise.
	var bare bytes.Buffer
	withAPIOutputWriter(t, &bare)
	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	outFlag, _ := c.fixedFlags.AddByName("output")
	outFlag.SetValue("table")
	if err := renderAPIOutput(c, nil, data()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bare.String(), "req-1") {
		t.Fatalf("bare table must not show RequestId:\n%s", bare.String())
	}

	// Explicit --query for that same field: the user asked for it, so it prints.
	var queried bytes.Buffer
	withAPIOutputWriter(t, &queried)
	c2 := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	outFlag2, _ := c2.fixedFlags.AddByName("output")
	outFlag2.SetValue("table")
	qFlag, _ := c2.fixedFlags.AddByName("query")
	qFlag.SetValue("ResponseMetadata.RequestId")
	if err := renderAPIOutput(c2, nil, data()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(queried.String(), "req-1") {
		t.Fatalf("explicit query must return the value:\n%s", queried.String())
	}

	// --query '@' asks for the whole response verbatim.
	var identity bytes.Buffer
	withAPIOutputWriter(t, &identity)
	c3 := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	outFlag3, _ := c3.fixedFlags.AddByName("output")
	outFlag3.SetValue("table")
	qFlag3, _ := c3.fixedFlags.AddByName("query")
	qFlag3.SetValue("@")
	if err := renderAPIOutput(c3, nil, data()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(identity.String(), "req-1") {
		t.Fatalf("--query '@' must keep the whole response:\n%s", identity.String())
	}
}

func TestRenderAPIOutputOff(t *testing.T) {
	var buf bytes.Buffer
	withAPIOutputWriter(t, &buf)

	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	outFlag, _ := c.fixedFlags.AddByName("output")
	outFlag.SetValue("off")
	if err := renderAPIOutput(c, nil, map[string]interface{}{"A": 1}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("off should be empty, got %q", buf.String())
	}
}

func TestRenderAPIOutputOffSkipsQueryEvaluation(t *testing.T) {
	var buf bytes.Buffer
	withAPIOutputWriter(t, &buf)

	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	outFlag, _ := c.fixedFlags.AddByName("output")
	outFlag.SetValue("off")
	qFlag, _ := c.fixedFlags.AddByName("query")
	qFlag.SetValue("max(A)")
	data := map[string]interface{}{"A": []interface{}{"not", "numbers"}}
	if err := renderAPIOutput(c, nil, data); err != nil {
		t.Fatalf("off output should skip query evaluation, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("off should be empty, got %q", buf.String())
	}
}

func TestRenderAPIOutputInvalidQuery(t *testing.T) {
	var buf bytes.Buffer
	withAPIOutputWriter(t, &buf)

	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	qFlag, _ := c.fixedFlags.AddByName("query")
	qFlag.SetValue("[[[")
	err := renderAPIOutput(c, nil, map[string]interface{}{"A": 1})
	if err == nil || !strings.Contains(err.Error(), "--query") {
		t.Fatalf("expected --query error, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("failed render must not write body: %q", buf.String())
	}
}

func TestRenderAPIOutputColoredJSONAppliesQuery(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var buf bytes.Buffer
	withAPIOutputWriter(t, &buf)

	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	qFlag, _ := c.fixedFlags.AddByName("query")
	qFlag.SetValue("Result.Id")
	data := map[string]interface{}{
		"Result": map[string]interface{}{"Id": "i-1", "Extra": "nope"},
	}
	if err := renderAPIOutput(c, &Configure{EnableColor: true}, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("colored query output missing ANSI: %q", out)
	}
	if !strings.Contains(out, "i-1") || strings.Contains(out, "Extra") {
		t.Fatalf("colored query did not project: %q", out)
	}
}

func TestRenderAPIOutputQueryEvaluationFailure(t *testing.T) {
	var buf bytes.Buffer
	withAPIOutputWriter(t, &buf)
	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	qFlag, _ := c.fixedFlags.AddByName("query")
	qFlag.SetValue("max(Result)")
	err := renderSuccessfulAPIOutput(mustPlan(t, c), nil, map[string]interface{}{"Result": map[string]interface{}{"A": 1}})
	if err == nil || !strings.Contains(err.Error(), "API call succeeded") || !strings.Contains(err.Error(), "--query evaluation failed") {
		t.Fatalf("expected post-call query error, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("failed query must not write body: %q", buf.String())
	}
}

func mustPlan(t *testing.T, c *Context) apiOutputPlan {
	t.Helper()
	plan, err := resolveAPIOutputPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestRenderAPIOutputColoredJSONUsesInjectedWriter(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var buf bytes.Buffer
	withAPIOutputWriter(t, &buf)

	c := &Context{fixedFlags: NewFlagSet(), dynamicFlags: NewFlagSet()}
	if err := renderAPIOutput(c, &Configure{EnableColor: true}, map[string]interface{}{"A": "b"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("colored output missing ANSI sequence: %q", buf.String())
	}
}

func TestRenderSuccessfulAPIOutputExplainsPostCallFailure(t *testing.T) {
	writerErr := errors.New("write failed")
	withAPIOutputWriter(t, writerFunc(func([]byte) (int, error) {
		return 0, writerErr
	}))

	err := renderSuccessfulAPIOutput(apiOutputPlan{format: output.FormatJSON}, nil, map[string]interface{}{"A": "b"})
	if !errors.Is(err, writerErr) {
		t.Fatalf("error = %v, want wrapped writer error", err)
	}
	if !strings.Contains(err.Error(), "API call succeeded") {
		t.Fatalf("error does not explain API success: %v", err)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

func TestOutputAndQueryAsSystemFlags(t *testing.T) {
	_, err := parseInvocationArgs([]string{
		"--output", "yaml",
		"--query", "Result.AccountId",
	}, map[string]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.fixedFlags.GetByName("output") == nil || ctx.fixedFlags.GetByName("output").GetValue() != "yaml" {
		t.Fatal("output not in fixedFlags")
	}
	if ctx.fixedFlags.GetByName("query") == nil || ctx.fixedFlags.GetByName("query").GetValue() != "Result.AccountId" {
		t.Fatal("query not in fixedFlags")
	}
}

func TestOutputConflictWithActionParameter(t *testing.T) {
	ctx.fixedFlags = NewFlagSet()
	ctx.dynamicFlags = NewFlagSet()
	p := NewParser([]string{"--output", "biz-value"}, map[string]struct{}{"output": {}})
	if _, err := p.ReadArgs(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.dynamicFlags.GetByName("output") == nil {
		t.Fatal("expected --output as business param")
	}
	if ctx.fixedFlags.GetByName("output") != nil {
		t.Fatal("system output should not be set on conflict")
	}

	ctx.fixedFlags = NewFlagSet()
	ctx.dynamicFlags = NewFlagSet()
	p = NewParser([]string{"---output", "table"}, map[string]struct{}{"output": {}})
	if _, err := p.ReadArgs(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.fixedFlags.GetByName("output") == nil || ctx.fixedFlags.GetByName("output").GetValue() != "table" {
		t.Fatal("---output should force system routing")
	}
}

func TestQueryConflictWithActionParameter(t *testing.T) {
	// insight.AgentChat exposes API parameter "query".
	ctx.fixedFlags = NewFlagSet()
	ctx.dynamicFlags = NewFlagSet()
	p := NewParser([]string{"--query", "biz-jmespath-looking"}, map[string]struct{}{"query": {}})
	if _, err := p.ReadArgs(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.dynamicFlags.GetByName("query") == nil {
		t.Fatal("expected --query as business param on conflict")
	}
	if ctx.fixedFlags.GetByName("query") != nil {
		t.Fatal("system query must not win on double-dash conflict")
	}

	ctx.fixedFlags = NewFlagSet()
	ctx.dynamicFlags = NewFlagSet()
	p = NewParser([]string{"---query", "Result.Id"}, map[string]struct{}{"query": {}})
	if _, err := p.ReadArgs(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.fixedFlags.GetByName("query") == nil || ctx.fixedFlags.GetByName("query").GetValue() != "Result.Id" {
		t.Fatal("---query should force system JMESPath routing")
	}
}
