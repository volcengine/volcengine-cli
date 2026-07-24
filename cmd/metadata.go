package cmd

import (
	"fmt"
	"strconv"
	"strings"
)

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

type VolcengineMeta struct {
	ApiInfo  *ApiInfo
	Request  *MetaInfo
	Response *MetaInfo
}

type MetaInfo struct {
	Basic     *[]string
	Structure *map[string]MetaInfo
}

type ApiInfo struct {
	Method      string
	ContentType string
	ServiceName string
	ParamTypes  map[string]string
	// int float64
	// [], {}
}

type StructInfo struct {
	PkgName     string
	ServiceName string
	Version     string
}

type param struct {
	key         string
	typeName    string
	required    bool
	description string // optional help text from param_descriptions asset
}

func formatParamsHelpUsage(params []param) []string {
	maxKeyLen := -1
	maxTypeNameLen := -1
	for _, p := range params {
		if len(p.key) > maxKeyLen {
			maxKeyLen = len(p.key)
		}
		if len(p.typeName) > maxTypeNameLen {
			maxTypeNameLen = len(p.typeName)
		}
	}

	maxKeyLen++
	maxTypeNameLen++

	// TODO: not print required field now
	// Widths are based only on key/type so long descriptions do not reflow columns.
	formatString := "%-" + strconv.Itoa(maxKeyLen) + "v%-" + strconv.Itoa(maxTypeNameLen) + "v"

	var paramStrings []string
	for _, p := range params {
		line := fmt.Sprintf(formatString, p.key, p.typeName)
		if desc := strings.TrimSpace(p.description); desc != "" {
			// Collapse multi-line OpenAPI text to one help line.
			if idx := strings.Index(desc, "\n"); idx >= 0 {
				desc = strings.TrimSpace(desc[:idx])
			}
			// Descriptions are baked into cobra text/templates; escape {{ }} so
			// OpenAPI text cannot break Usage() parsing.
			desc = escapeCobraTemplateLiteral(desc)
			if desc != "" {
				line = line + " " + desc
			}
		}
		paramStrings = append(paramStrings, line)
	}

	return paramStrings
}

// escapeCobraTemplateLiteral makes free-form text safe to embed in a cobra
// usage template that is later parsed as text/template.
func escapeCobraTemplateLiteral(s string) string {
	// {{"{{"}} and {{"}}"}} evaluate to literal braces in text/template.
	s = strings.ReplaceAll(s, "{{", `{{"{{"}}`)
	s = strings.ReplaceAll(s, "}}", `{{"}}"}}`)
	return s
}

func formatRequired(required bool) string {
	if required {
		return "Required"
	}
	return "Optional"
}

func (meta *VolcengineMeta) GetRequestParams(apiMeta *ApiMeta) (params []param) {
	var s []string
	var traverse func(MetaInfo)

	traverse = func(meta MetaInfo) {
		if meta.Basic != nil {
			for _, v := range *meta.Basic {
				s = append(s, v)
				if apiMeta == nil {
					paramKey := strings.Join(s, ".")
					params = append(params, param{
						key:      paramKey,
						typeName: "",
						required: false,
					})
				} else {
					paramKey := strings.Join(s, ".")
					params = append(params, param{
						key:      paramKey,
						typeName: apiMeta.GetReqTypeName(paramKey),
						required: apiMeta.GetReqRequired(paramKey),
					})
				}
				s = s[:len(s)-1]
			}
		}

		if meta.Structure != nil {
			for k2, v2 := range *meta.Structure {
				s = append(s, k2)
				traverse(v2)
				s = s[:len(s)-1]
			}
		}
	}

	traverse(*meta.Request)
	return
}
