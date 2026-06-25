package cmd

import (
	"reflect"
	"testing"
)

// arrayObjectChildMetaReq mirrors an array<object> nested under an object, where
// the element field types live in ChildMetas (e.g. NasStorage.NasConfigs[].Uid),
// plus an array<scalar> field under the same object (NasStorage.Ports).
func arrayObjectChildMetaReq() *ApiMeta {
	return &ApiMeta{Request: &Meta{
		MetaTypes: map[string]*MetaType{"NasStorage": {TypeName: "object"}},
		ChildMetas: map[string]*Meta{
			"NasStorage": {
				MetaTypes: map[string]*MetaType{
					"NasConfigs": {TypeName: "array", TypeOf: "object"},
					"Ports":      {TypeName: "array", TypeOf: "integer"},
				},
				ChildMetas: map[string]*Meta{
					"NasConfigs": {MetaTypes: map[string]*MetaType{
						"Uid": {TypeName: "integer"},
						"Gid": {TypeName: "integer"},
					}},
				},
			},
		},
	}}
}

func TestExpandFlatToJSONArrayObjectChildMetas(t *testing.T) {
	got, err := expandFlatToJSON(map[string]string{
		"NasStorage.NasConfigs.1.Uid": "1000",
		"NasStorage.NasConfigs.1.Gid": "2000",
		"NasStorage.NasConfigs.2.Uid": "1001",
	}, arrayObjectChildMetaReq())
	if err != nil {
		t.Fatalf("expandFlatToJSON returned error: %v", err)
	}
	want := map[string]interface{}{"NasStorage": map[string]interface{}{
		"NasConfigs": []interface{}{
			map[string]interface{}{"Uid": int64(1000), "Gid": int64(2000)},
			map[string]interface{}{"Uid": int64(1001)},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("array<object> ChildMetas\n got = %#v\nwant = %#v", got, want)
	}
}

func TestExpandFlatToJSONScalarArrayUnderObject(t *testing.T) {
	got, err := expandFlatToJSON(map[string]string{
		"NasStorage.Ports.1": "80",
		"NasStorage.Ports.2": "443",
	}, arrayObjectChildMetaReq())
	if err != nil {
		t.Fatalf("expandFlatToJSON returned error: %v", err)
	}
	want := map[string]interface{}{"NasStorage": map[string]interface{}{
		"Ports": []interface{}{int64(80), int64(443)},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("array<scalar> under object\n got = %#v\nwant = %#v", got, want)
	}
}
