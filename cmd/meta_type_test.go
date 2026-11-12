package cmd

import (
	"reflect"
	"testing"
)

// Scalar arrays in the --help body skeleton must be JSON arrays holding one
// element placeholder, not the type name string "array".
func TestGetReqBodyScalarArraySkeleton(t *testing.T) {
	m := &Meta{MetaTypes: map[string]*MetaType{
		"Names":   {TypeName: "array", TypeOf: "string"},
		"Ports":   {TypeName: "array", TypeOf: "integer"},
		"Enabled": {TypeName: "array", TypeOf: "boolean"},
	}}
	want := map[string]interface{}{
		"Names":   []interface{}{"string"},
		"Ports":   []interface{}{0},
		"Enabled": []interface{}{false},
	}
	if got := m.GetReqBody(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GetReqBody() = %#v, want %#v", got, want)
	}
}
