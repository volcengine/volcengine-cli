package cmd

import (
	"reflect"
	"testing"
)

func testReqMeta() *ApiMeta {
	return &ApiMeta{Request: &Meta{MetaTypes: map[string]*MetaType{
		"InstanceId":      {TypeName: "string"},
		"Limit":           {TypeName: "integer"},
		"Offset":          {TypeName: "long"},
		"Enabled":         {TypeName: "boolean"},
		"Ratio":           {TypeName: "number"},
		"Filters.N.Key":   {TypeName: "string"},
		"Filters.N.Value": {TypeName: "string"},
		"ResourceNames":   {TypeName: "array[string]"},
		"ResourceNames.N": {TypeName: "array[string]"},
		"IPList":          {TypeName: "array", TypeOf: "string"},
		"IPList.N":        {TypeName: "array", TypeOf: "string"},
		"Ports":           {TypeName: "array", TypeOf: "integer"},
		"Ports.N":         {TypeName: "array", TypeOf: "integer"},
		"Config":          {TypeName: "object"},
	}}}
}

// childMetaReq mirrors how nested object fields are stored: the object type is
// in MetaTypes, while its sub-fields live in ChildMetas (as GetReqTypeName /
// GetRequestParams traverse for the --help display).
func childMetaReq() *ApiMeta {
	return &ApiMeta{Request: &Meta{
		MetaTypes: map[string]*MetaType{
			"AsyncTaskConfig": {TypeName: "object"},
		},
		ChildMetas: map[string]*Meta{
			"AsyncTaskConfig": {MetaTypes: map[string]*MetaType{
				"MaxRetry": {TypeName: "integer"},
				"Enabled":  {TypeName: "boolean"},
			}},
		},
	}}
}

func TestExpandFlatToJSON(t *testing.T) {
	tests := []struct {
		name string
		flat map[string]string
		want map[string]interface{}
	}{
		{
			name: "scalars typed from metadata",
			flat: map[string]string{"InstanceId": "mysql-1", "Limit": "20", "Offset": "5", "Enabled": "true", "Ratio": "0.5"},
			want: map[string]interface{}{"InstanceId": "mysql-1", "Limit": int64(20), "Offset": int64(5), "Enabled": true, "Ratio": 0.5},
		},
		{
			name: "unknown key stays string",
			flat: map[string]string{"Whatever": "x"},
			want: map[string]interface{}{"Whatever": "x"},
		},
		{
			name: "object array via dotted keys",
			flat: map[string]string{"Filters.1.Key": "env", "Filters.1.Value": "prod", "Filters.2.Key": "app"},
			want: map[string]interface{}{"Filters": []interface{}{
				map[string]interface{}{"Key": "env", "Value": "prod"},
				map[string]interface{}{"Key": "app"},
			}},
		},
		{
			name: "scalar array via dotted indices (array[string] form)",
			flat: map[string]string{"ResourceNames.1": "a", "ResourceNames.2": "b"},
			want: map[string]interface{}{"ResourceNames": []interface{}{"a", "b"}},
		},
		{
			name: "scalar array element with array+TypeOf string",
			flat: map[string]string{"IPList.1": "1.1.1.1", "IPList.2": "2.2.2.2"},
			want: map[string]interface{}{"IPList": []interface{}{"1.1.1.1", "2.2.2.2"}},
		},
		{
			name: "scalar array element typed by TypeOf integer",
			flat: map[string]string{"Ports.1": "80", "Ports.2": "443"},
			want: map[string]interface{}{"Ports": []interface{}{int64(80), int64(443)}},
		},
		{
			name: "whole-JSON array value (array[string] form)",
			flat: map[string]string{"ResourceNames": `["a","b"]`},
			want: map[string]interface{}{"ResourceNames": []interface{}{"a", "b"}},
		},
		{
			name: "whole-JSON array value (array+TypeOf form)",
			flat: map[string]string{"IPList": `["1.1.1.1","2.2.2.2"]`},
			want: map[string]interface{}{"IPList": []interface{}{"1.1.1.1", "2.2.2.2"}},
		},
		{
			name: "whole-JSON object value",
			flat: map[string]string{"Config": `{"a":1}`},
			want: map[string]interface{}{"Config": map[string]interface{}{"a": float64(1)}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandFlatToJSON(tt.flat, testReqMeta())
			if err != nil {
				t.Fatalf("expandFlatToJSON returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expandFlatToJSON()\n got = %#v\nwant = %#v", got, tt.want)
			}
		})
	}
}

// TestExpandFlatToJSONChildMetas covers nested object fields whose type lives in
// ChildMetas (Bug 1: AsyncTaskConfig.MaxRetry must be a number, not a string).
func TestExpandFlatToJSONChildMetas(t *testing.T) {
	got, err := expandFlatToJSON(map[string]string{
		"AsyncTaskConfig.MaxRetry": "3",
		"AsyncTaskConfig.Enabled":  "true",
	}, childMetaReq())
	if err != nil {
		t.Fatalf("expandFlatToJSON returned error: %v", err)
	}
	want := map[string]interface{}{"AsyncTaskConfig": map[string]interface{}{
		"MaxRetry": int64(3),
		"Enabled":  true,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expandFlatToJSON()\n got = %#v\nwant = %#v", got, want)
	}
}

func TestExpandFlatToJSONErrors(t *testing.T) {
	tests := []struct {
		name string
		flat map[string]string
	}{
		{name: "conversion failure", flat: map[string]string{"Limit": "abc"}},
		{name: "array element conversion failure", flat: map[string]string{"Ports.1": "notint"}},
		{name: "index gap", flat: map[string]string{"ResourceNames.1": "a", "ResourceNames.3": "b"}},
		{name: "index zero", flat: map[string]string{"ResourceNames.0": "a"}},
		{name: "negative index", flat: map[string]string{"ResourceNames.-1": "a"}},
		{name: "plus-signed index", flat: map[string]string{"ResourceNames.+1": "a"}},
		{name: "leaf vs branch conflict", flat: map[string]string{"Filters": "x", "Filters.1.Key": "y"}},
		{name: "object/array mix", flat: map[string]string{"Filters.1.Key": "a", "Filters.Other": "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := expandFlatToJSON(tt.flat, testReqMeta()); err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}
