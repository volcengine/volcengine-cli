package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/volcengine/volcengine-cli/util/output"
)

func withAPIOutputWriter(t *testing.T, w *bytes.Buffer) {
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
