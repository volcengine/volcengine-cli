package paramdescriptions

import (
	"encoding/json"
	"testing"
)

func TestEmbeddedParamsJSON(t *testing.T) {
	b, err := Asset("params.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 1000000 {
		t.Fatalf("asset too small: %d", len(b))
	}
	var d struct {
		Apis map[string]map[string]map[string]map[string]json.RawMessage `json:"apis"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Apis) < 100 {
		t.Fatalf("services=%d, want >=100", len(d.Apis))
	}
	// bilingual sample
	zone := d.Apis["ecs"]["2020-04-01"]["RunInstances"]["ZoneId"]
	if zone == nil {
		t.Fatal("missing ecs RunInstances ZoneId")
	}
	var pd map[string]interface{}
	if err := json.Unmarshal(zone, &pd); err != nil {
		t.Fatal(err)
	}
	if pd["description_cn"] == nil && pd["description_en"] == nil {
		t.Fatalf("no descriptions: %v", pd)
	}
	t.Logf("services=%d bytes=%d zone=%v", len(d.Apis), len(b), pd)
}
