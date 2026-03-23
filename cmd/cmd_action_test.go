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
