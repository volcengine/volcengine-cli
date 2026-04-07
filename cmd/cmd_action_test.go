package cmd

import "testing"

func TestIsStringParam(t *testing.T) {
	tests := []struct {
		name     string
		apiMeta  *ApiMeta
		param    string
		expected bool
	}{
		{
			name:     "nil apiMeta",
			apiMeta:  nil,
			param:    "PolicyDocument",
			expected: false,
		},
		{
			name: "string type param",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"PolicyDocument": {TypeName: "string"},
						"PolicyName":     {TypeName: "string"},
					},
				},
			},
			param:    "PolicyDocument",
			expected: true,
		},
		{
			name: "non-string type param",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"Filters": {TypeName: "object"},
					},
				},
			},
			param:    "Filters",
			expected: false,
		},
		{
			name: "unknown param",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"PolicyName": {TypeName: "string"},
					},
				},
			},
			param:    "Unknown",
			expected: false,
		},
		{
			name: "indexed repeated string param",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"TagFilters.N.Key": {TypeName: "string"},
					},
				},
			},
			param:    "TagFilters.1.Key",
			expected: true,
		},
		{
			name: "indexed repeated string array element",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"TagFilters.N.Values.N": {TypeName: "array[string]"},
					},
				},
			},
			param:    "TagFilters.1.Values.1",
			expected: true,
		},
		{
			name: "indexed root string array element",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"ResourceNames.N": {TypeName: "array[string]"},
					},
				},
			},
			param:    "ResourceNames.0",
			expected: true,
		},
		{
			name: "root string array is not treated as string literal",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"ResourceNames": {TypeName: "array[string]"},
					},
				},
			},
			param:    "ResourceNames",
			expected: false,
		},
		{
			name: "indexed repeated non-string param",
			apiMeta: &ApiMeta{
				Request: &Meta{
					MetaTypes: map[string]*MetaType{
						"TagFilters.N": {TypeName: "object"},
					},
				},
			},
			param:    "TagFilters.1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStringParam(tt.apiMeta, tt.param)
			if got != tt.expected {
				t.Errorf("isStringParam(%v, %q) = %v, want %v", tt.apiMeta, tt.param, got, tt.expected)
			}
		})
	}
}
