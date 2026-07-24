package cmd

import (
	"fmt"
	"sort"
	"strings"
)

type mcpServiceResult struct {
	Service     string `json:"service"`
	NameCN      string `json:"name_cn,omitempty"`
	Description string `json:"description,omitempty"`
}

type mcpAPIResult struct {
	Service     string `json:"service"`
	API         string `json:"api"`
	NameCN      string `json:"name_cn,omitempty"`
	NameEN      string `json:"name_en,omitempty"`
	Description string `json:"description,omitempty"`
}

type mcpParameter struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required"`
}

type mcpAPIParameters struct {
	Service     string                 `json:"service"`
	API         string                 `json:"api"`
	Method      string                 `json:"method"`
	Version     string                 `json:"version"`
	ContentType string                 `json:"content_type,omitempty"`
	Parameters  []mcpParameter         `json:"parameters"`
	Example     map[string]interface{} `json:"example,omitempty"`
}

func listMCPServices(support *RootSupport) []mcpServiceResult {
	if support == nil {
		return nil
	}
	seen := make(map[string]struct{})
	results := make([]mcpServiceResult, 0, len(support.GetAllSvc()))
	for _, service := range support.GetAllSvc() {
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		description := getServiceDescription(service)
		results = append(results, mcpServiceResult{
			Service: service, NameCN: description.ServiceCn, Description: description.ServiceEn,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Service < results[j].Service })
	return results
}

func listMCPAPIs(support *RootSupport, service string) ([]mcpAPIResult, error) {
	if support == nil || !support.IsValidSvc(service) {
		return nil, fmt.Errorf("unsupported service %q", service)
	}
	results := make([]mcpAPIResult, 0, len(support.GetAllAction(service)))
	for _, api := range support.GetAllAction(service) {
		description := getApiDescription(service, api)
		text := firstNonEmpty(description.DescriptionEn, description.DescriptionCn, description.Description)
		results = append(results, mcpAPIResult{
			Service: service, API: api, NameCN: description.NameCn, NameEN: description.NameEn, Description: firstLine(text),
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].API < results[j].API })
	return results, nil
}

func getMCPAPIParameters(support *RootSupport, service, api string) (*mcpAPIParameters, error) {
	if support == nil || !support.IsValidAction(service, api) {
		return nil, fmt.Errorf("unsupported API %s.%s", service, api)
	}
	info := support.GetApiInfo(service, api)
	method := "GET"
	contentType := ""
	if info != nil {
		if info.Method != "" {
			method = info.Method
		}
		contentType = info.ContentType
	}
	meta := support.GetApiMeta(service, api)
	params := make([]mcpParameter, 0)
	if meta != nil {
		for _, parameter := range meta.GetRequestParams() {
			params = append(params, mcpParameter{Name: parameter.key, Type: strings.TrimSpace(parameter.typeName), Required: parameter.required})
		}
	}
	result := &mcpAPIParameters{
		Service: service, API: api, Method: method, Version: support.GetVersion(service),
		ContentType: contentType, Parameters: params,
	}
	if meta != nil && meta.Request != nil {
		result.Example = meta.Request.GetReqBody()
	}
	return result, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
