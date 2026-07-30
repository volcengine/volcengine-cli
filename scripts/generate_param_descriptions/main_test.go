// Tests for S14 merge/prune helpers in generate_param_descriptions.
// Run:
//
//	go test -count=1 ./scripts/generate_param_descriptions
package main

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeParamsFileKeepsUnscannedActions(t *testing.T) {
	prev := paramsFile{Apis: map[string]map[string]map[string]map[string]paramDescription{
		"ecs": {
			"2020-04-01": {
				"RunInstances": {
					"ZoneId": {DescriptionCn: "旧可用区", DescriptionEn: "old zone", ExampleCn: "cn-a", ExampleEn: "cn-a"},
				},
				"DescribeInstances": {
					"PageSize": {DescriptionCn: "页大小"},
				},
			},
		},
		"vpc": {
			"2020-04-01": {
				"CreateVpc": {
					"CidrBlock": {DescriptionCn: "网段", ExampleCn: "10.0.0.0/16"},
				},
			},
		},
	}}
	// Partial fetch: only one ecs action updated; vpc not scanned.
	patch := paramsFile{Apis: map[string]map[string]map[string]map[string]paramDescription{
		"ecs": {
			"2020-04-01": {
				"RunInstances": {
					"ZoneId": {DescriptionCn: "新可用区", DescriptionEn: "new zone"}, // examples empty → keep prev
				},
			},
		},
	}}

	merged, stats := mergeParamsFile(prev, patch)
	if stats.MergedActions != 1 {
		t.Fatalf("MergedActions=%d want 1", stats.MergedActions)
	}
	if stats.KeptPrevActions != 2 {
		t.Fatalf("KeptPrevActions=%d want 2 (DescribeInstances + CreateVpc)", stats.KeptPrevActions)
	}

	zone := merged.Apis["ecs"]["2020-04-01"]["RunInstances"]["ZoneId"]
	if zone.DescriptionCn != "新可用区" || zone.DescriptionEn != "new zone" {
		t.Fatalf("description not updated: %+v", zone)
	}
	if zone.ExampleCn != "cn-a" || zone.ExampleEn != "cn-a" {
		t.Fatalf("examples should be preserved from prev: %+v", zone)
	}
	if _, ok := merged.Apis["ecs"]["2020-04-01"]["DescribeInstances"]; !ok {
		t.Fatal("unscanned action DescribeInstances was wiped")
	}
	if _, ok := merged.Apis["vpc"]["2020-04-01"]["CreateVpc"]; !ok {
		t.Fatal("unscanned service vpc was wiped")
	}
}

func TestMergeActionParamsLanguageHalfSuccess(t *testing.T) {
	prev := map[string]paramDescription{
		"Name": {
			DescriptionCn: "旧中文",
			DescriptionEn: "old en",
			ExampleCn:     "例",
			ExampleEn:     "ex",
		},
	}
	// zh-only fetch
	next := map[string]paramDescription{
		"Name": {DescriptionCn: "新中文", ExampleCn: "新例"},
	}
	got := mergeActionParams(prev, next)
	if got["Name"].DescriptionCn != "新中文" {
		t.Fatalf("cn desc: %+v", got["Name"])
	}
	if got["Name"].DescriptionEn != "old en" {
		t.Fatalf("en desc should keep prev: %+v", got["Name"])
	}
	if got["Name"].ExampleEn != "ex" {
		t.Fatalf("en example should keep prev: %+v", got["Name"])
	}
	if got["Name"].ExampleCn != "新例" {
		t.Fatalf("cn example: %+v", got["Name"])
	}
}

func TestMergeActionParamsKeepsPrevOnlyParams(t *testing.T) {
	prev := map[string]paramDescription{
		"A": {DescriptionCn: "a"},
		"B": {DescriptionCn: "b"},
	}
	next := map[string]paramDescription{
		"A": {DescriptionCn: "a2"},
	}
	got := mergeActionParams(prev, next)
	if got["A"].DescriptionCn != "a2" || got["B"].DescriptionCn != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestPruneParamsToInventory(t *testing.T) {
	file := paramsFile{Apis: map[string]map[string]map[string]map[string]paramDescription{
		"ecs": {
			"2020-04-01": {"RunInstances": {"Z": {DescriptionCn: "z"}}},
			"2019-01-01": {"Old": {"X": {DescriptionCn: "x"}}},
		},
		"gone": {
			"2020-01-01": {"Act": {"P": {DescriptionCn: "p"}}},
		},
	}}
	inventory := map[string][]string{
		"ecs": {"2020-04-01"},
	}
	n := pruneParamsToInventory(file, inventory)
	if n != 2 {
		t.Fatalf("pruned=%d want 2 (ecs@2019 + gone@2020)", n)
	}
	if _, ok := file.Apis["ecs"]["2020-04-01"]; !ok {
		t.Fatal("kept version missing")
	}
	if _, ok := file.Apis["ecs"]["2019-01-01"]; ok {
		t.Fatal("old version should be pruned")
	}
	if _, ok := file.Apis["gone"]; ok {
		t.Fatal("gone service should be pruned")
	}
}

func TestMergeParamsFileEmptyPatchKeepsBase(t *testing.T) {
	prev := paramsFile{Apis: map[string]map[string]map[string]map[string]paramDescription{
		"ecs": {"2020-04-01": {"A": {"P": {DescriptionCn: "p"}}}},
	}}
	merged, stats := mergeParamsFile(prev, paramsFile{})
	if stats.MergedActions != 0 || stats.KeptPrevActions != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	if countActions(merged) != 1 {
		t.Fatalf("count=%d", countActions(merged))
	}
}

func TestLoadPreviousCorpusMissingOK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-params.json")
	file, loaded, err := loadPreviousCorpus(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded {
		t.Fatal("expected loaded=false for missing file")
	}
	if file.Apis == nil || countActions(file) != 0 {
		t.Fatalf("expected empty corpus, got %+v", file)
	}
}

func TestLoadPreviousCorpusCorruptRefuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "params.json")
	if err := ioutil.WriteFile(path, []byte("{not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, loaded, err := loadPreviousCorpus(path)
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
	if loaded {
		t.Fatal("loaded must be false on error")
	}
}

func TestLoadPreviousCorpusValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "params.json")
	body := `{"apis":{"ecs":{"2020-04-01":{"RunInstances":{"ZoneId":{"description_cn":"z"}}}}}}`
	if err := ioutil.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	file, loaded, err := loadPreviousCorpus(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded {
		t.Fatal("expected loaded=true")
	}
	if file.Apis["ecs"]["2020-04-01"]["RunInstances"]["ZoneId"].DescriptionCn != "z" {
		t.Fatalf("unexpected corpus: %+v", file)
	}
}

func TestParamDescriptionIgnoresLegacyRequiredField(t *testing.T) {
	var p paramDescription
	if err := json.Unmarshal([]byte(`{"description_en":"name","required":true}`), &p); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "required") {
		t.Fatalf("legacy required field leaked into regenerated corpus: %s", data)
	}
	if p.DescriptionEn != "name" {
		t.Fatalf("description lost while ignoring required: %+v", p)
	}
}

func TestMergeActionParamsWhitespaceDoesNotClear(t *testing.T) {
	prev := map[string]paramDescription{
		"Name": {DescriptionCn: "旧", DescriptionEn: "old", ExampleCn: "例"},
	}
	next := map[string]paramDescription{
		"Name": {DescriptionCn: "  ", DescriptionEn: "", ExampleCn: "\t"},
	}
	got := mergeActionParams(prev, next)
	if got["Name"].DescriptionCn != "旧" || got["Name"].DescriptionEn != "old" || got["Name"].ExampleCn != "例" {
		t.Fatalf("whitespace should not clear prev: %+v", got["Name"])
	}
}
