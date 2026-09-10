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

func TestMergeParamLangKeepsRequirement(t *testing.T) {
	dst := map[string]paramDescription{}
	src := map[string]paramDescription{
		"Name": {
			DescriptionCn: "名称",
			Requirement:   requirementRequired,
		},
	}
	mergeParamLang(dst, src, "zh")
	if got := dst["Name"].Requirement; got != requirementRequired {
		t.Fatalf("requirement = %q, want %q", got, requirementRequired)
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

func TestExtractParamsPreservesReferenceAnnotations(t *testing.T) {
	var doc openAPIDoc
	body := `{
  "components": {
    "schemas": {
      "Config": {
        "description": "component description",
        "properties": {
          "Enabled": {"description": "enabled description", "type": "boolean"}
        },
        "type": "object"
      }
    }
  },
  "paths": {
    "/Create": {
      "post": {
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "properties": {
                  "Config": {
                    "$ref": "#/components/schemas/Config",
                    "description": "reference-site description",
                    "example": {"Enabled": true}
                  }
                },
                "type": "object"
              }
            }
          }
        }
      }
    }
  }
}`
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}

	params := extractParamsFromOpenAPI(&doc, "en")
	if got := params["Config"].DescriptionEn; got != "reference-site description" {
		t.Fatalf("Config description = %q, want reference-site description", got)
	}
	if got := params["Config"].ExampleEn; got != `{"Enabled": true}` {
		t.Fatalf("Config example = %q", got)
	}
	if got := params["Config.Enabled"].DescriptionEn; got != "enabled description" {
		t.Fatalf("nested description = %q", got)
	}
}

func TestExtractParamsCapturesSwaggerRequirements(t *testing.T) {
	var doc openAPIDoc
	body := `{
  "components": {
    "schemas": {
      "Config": {
        "properties": {
          "Enabled": {"type": "boolean"},
          "Label": {"type": "string"}
        },
        "required": ["Enabled"],
        "type": "object"
      }
    }
  },
  "paths": {
    "/Create": {
      "post": {
        "parameters": [
          {"name": "ProjectName", "required": true, "schema": {"type": "string"}}
        ],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "properties": {
                  "Name": {"type": "string"},
                  "Config": {"$ref": "#/components/schemas/Config"},
                  "RequiredConfig": {"$ref": "#/components/schemas/Config"}
                },
                "required": ["Name", "RequiredConfig"],
                "type": "object"
              }
            }
          }
        }
      }
    }
  }
}`
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}

	params := extractParamsFromOpenAPI(&doc, "en")
	wants := map[string]string{
		"ProjectName":            "required",
		"Name":                   "required",
		"Config":                 "optional",
		"Config.Enabled":         "conditional",
		"Config.Label":           "optional",
		"RequiredConfig":         "required",
		"RequiredConfig.Enabled": "required",
		"RequiredConfig.Label":   "optional",
	}
	for name, want := range wants {
		data, err := json.Marshal(params[name])
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]interface{}
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if got, _ := fields["requirement"].(string); got != want {
			t.Errorf("%s requirement = %q, want %q; data=%s", name, got, want, data)
		}
	}
}

func TestExtractParamsExpandsStructuredQueryParameters(t *testing.T) {
	var doc openAPIDoc
	body := `{
  "components": {
    "schemas": {
      "Entry": {
        "properties": {
          "Description": {"description": "entry description", "type": "string"},
          "Labels": {
            "description": "entry labels",
            "items": {"type": "string"},
            "type": "array"
          },
          "Value": {"description": "entry value", "type": "string"}
        },
        "required": ["Value"],
        "type": "object"
      }
    }
  },
  "paths": {
    "/Add": {
      "get": {
        "parameters": [
          {
            "description": "entries",
            "name": "Entries",
            "required": true,
            "schema": {
              "items": {"$ref": "#/components/schemas/Entry"},
              "type": "array"
            }
          },
          {
            "description": "ids",
            "name": "Ids",
            "required": true,
            "schema": {
              "items": {"type": "string"},
              "type": "array"
            }
          }
        ]
      }
    }
  }
}`
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}

	params := extractParamsFromOpenAPI(&doc, "en")
	if got := params["Entries.N.Description"].DescriptionEn; got != "entry description" {
		t.Errorf("Entries.N.Description = %q", got)
	}
	if got := params["Entries.N.Value"].DescriptionEn; got != "entry value" {
		t.Errorf("Entries.N.Value = %q", got)
	}
	if got := params["Entries.N.Value"].Requirement; got != requirementRequired {
		t.Errorf("Entries.N.Value requirement = %q, want %q", got, requirementRequired)
	}
	if got := params["Entries.N.Labels.N"]; got.DescriptionEn != "entry labels" || got.Requirement != requirementOptional {
		t.Errorf("Entries.N.Labels.N = %+v", got)
	}
	if got := params["Ids.N"]; got.DescriptionEn != "ids" || got.Requirement != requirementRequired {
		t.Errorf("Ids.N = %+v", got)
	}
}

func TestExtractParamsPropagatesRequiredAncestor(t *testing.T) {
	var doc openAPIDoc
	body := `{
  "components": {
    "schemas": {
      "NetworkInterface": {
        "properties": {
          "SubnetId": {"description": "subnet", "type": "string"}
        },
        "required": ["SubnetId"],
        "type": "object"
      }
    }
  },
  "paths": {
    "/Run": {
      "get": {
        "parameters": [
          {
            "name": "NetworkInterfaces",
            "required": true,
            "schema": {
              "items": {"$ref": "#/components/schemas/NetworkInterface"},
              "type": "array"
            }
          },
          {
            "name": "OptionalInterfaces",
            "required": false,
            "schema": {
              "items": {"$ref": "#/components/schemas/NetworkInterface"},
              "type": "array"
            }
          }
        ]
      }
    }
  }
}`
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}

	params := extractParamsFromOpenAPI(&doc, "en")
	if got := params["NetworkInterfaces.N.SubnetId"].Requirement; got != requirementRequired {
		t.Errorf("required parent child requirement = %q, want %q", got, requirementRequired)
	}
	if got := params["OptionalInterfaces.N.SubnetId"].Requirement; got != requirementConditional {
		t.Errorf("optional parent child requirement = %q, want %q", got, requirementConditional)
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

func TestCollectSchemaParamsStopsOnCyclicRef(t *testing.T) {
	// Node.Children[] → Node (self-ref via components/schemas).
	node := &schemaNode{
		Type: "object",
		Properties: map[string]*schemaNode{
			"Name": {Type: "string", Description: "node name"},
			"Children": {
				Type:        "array",
				Description: "child nodes",
				Items: &schemaNode{
					Ref: "#/components/schemas/Node",
				},
			},
		},
	}
	schemas := map[string]*schemaNode{"Node": node}
	out := map[string]paramDescription{}
	// Must return (not stack-overflow) and still record top-level fields.
	collectSchemaParams("", node, schemas, out, "en")
	if _, ok := out["Name"]; !ok {
		t.Fatalf("expected Name param, got %#v", out)
	}
	if got, ok := out["Children"]; !ok || got.DescriptionEn != "child nodes" {
		t.Fatalf("expected Children param with description, got %#v", out)
	}
	// Cycle break must not invent infinite Children.Children... keys.
	for k := range out {
		if strings.Count(k, "Children") > 1 {
			t.Fatalf("unexpected deep cycle key %q in %#v", k, out)
		}
	}
}

func TestCollectSchemaParamsStopsOnMutualRef(t *testing.T) {
	// A.BRef → B, B.ARef → A (mutual $ref cycle).
	a := &schemaNode{
		Type: "object",
		Properties: map[string]*schemaNode{
			"AName": {Type: "string", Description: "a name"},
			"BRef":  {Ref: "#/components/schemas/B"},
		},
	}
	b := &schemaNode{
		Type: "object",
		Properties: map[string]*schemaNode{
			"BName": {Type: "string", Description: "b name"},
			"ARef":  {Ref: "#/components/schemas/A"},
		},
	}
	schemas := map[string]*schemaNode{"A": a, "B": b}
	out := map[string]paramDescription{}
	collectSchemaParams("", a, schemas, out, "en")
	if _, ok := out["AName"]; !ok {
		t.Fatalf("expected AName, got %#v", out)
	}
	// B is expanded once under BRef; mutual back-edge must not overflow.
	if _, ok := out["BRef.BName"]; !ok {
		// Depending on putParam + nesting, BName may appear as BRef.BName when B expands.
		// If BRef itself has no description, only nested fields with desc appear.
		foundBName := false
		for k := range out {
			if strings.HasSuffix(k, "BName") || k == "BName" {
				foundBName = true
				break
			}
		}
		if !foundBName {
			t.Fatalf("expected nested BName under cycle-safe walk, got %#v", out)
		}
	}
	for k := range out {
		if strings.Count(k, "ARef") > 1 || strings.Count(k, "BRef") > 1 {
			t.Fatalf("unexpected deep mutual-ref key %q in %#v", k, out)
		}
	}
}
