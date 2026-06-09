package cmd

import "testing"

func TestBuildActionInputRejectsBodyWithFlattenedParams(t *testing.T) {
	body := &Flag{Name: "body"}
	body.SetValue(`{"InstanceId":"mysql-1"}`)
	instanceID := &Flag{Name: "InstanceId"}
	instanceID.SetValue("mysql-1")

	_, _, err := buildActionInput([]*Flag{body, instanceID}, nil, true)
	if err == nil {
		t.Fatal("expected --body and flattened params conflict")
	}
}

func TestBuildActionInputParsesJsonBody(t *testing.T) {
	body := &Flag{Name: "body"}
	body.SetValue(`{"InstanceId":"mysql-1","IPList":["10.20.30.40"]}`)

	input, fromBody, err := buildActionInput([]*Flag{body}, nil, true)
	if err != nil {
		t.Fatalf("buildActionInput returned error: %v", err)
	}
	if !fromBody {
		t.Fatal("expected input to be marked from body")
	}
	m, ok := input.(*map[string]interface{})
	if !ok {
		t.Fatalf("expected *map input, got %T", input)
	}
	if (*m)["InstanceId"] != "mysql-1" {
		t.Fatalf("expected InstanceId to be parsed, got %#v", (*m)["InstanceId"])
	}
}

func TestBuildActionInputSupportsFlattenedJsonBodyParams(t *testing.T) {
	apiMeta := &ApiMeta{Request: &Meta{MetaTypes: map[string]*MetaType{
		"InstanceId": {TypeName: "string"},
		"GroupName":  {TypeName: "string"},
		"IPList":     {TypeName: "array[string]"},
	}}}

	instanceID := &Flag{Name: "InstanceId"}
	instanceID.SetValue("mysql-1")
	groupName := &Flag{Name: "GroupName"}
	groupName.SetValue("group-a")
	ipList := &Flag{Name: "IPList"}
	ipList.SetValue(`["10.20.30.40","50.60.70.80"]`)

	input, fromBody, err := buildActionInput([]*Flag{instanceID, groupName, ipList}, apiMeta, true)
	if err != nil {
		t.Fatalf("buildActionInput returned error: %v", err)
	}
	if fromBody {
		t.Fatal("flattened params should not be marked from body")
	}
	m, ok := input.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map input, got %T", input)
	}
	if m["InstanceId"] != "mysql-1" || m["GroupName"] != "group-a" {
		t.Fatalf("unexpected scalar params: %#v", m)
	}
	gotList, ok := m["IPList"].([]interface{})
	if !ok || len(gotList) != 2 {
		t.Fatalf("expected IPList JSON array, got %#v", m["IPList"])
	}
}

func TestFormatActionErrorNoCredentialProviders(t *testing.T) {
	err := formatActionError(assertErr("NoCredentialProviders: no valid providers in chain. Deprecated."))
	if err == nil {
		t.Fatal("expected formatted error")
	}
	if got := err.Error(); got != "credentials not configured, please run 've login' or 've configure set', or set VOLCENGINE_ACCESS_KEY and VOLCENGINE_SECRET_KEY environment variables" {
		t.Fatalf("unexpected error: %q", got)
	}
}

type assertErr string

func (e assertErr) Error() string {
	return string(e)
}

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
