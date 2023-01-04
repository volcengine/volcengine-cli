package cmd

import "strings"

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

func (meta *VolcengineMeta) GetRequestParams(apiMeta *ApiMeta) (params []string, onlyKepParams []string) {
	var s []string
	var traverse func(MetaInfo)

	traverse = func(meta MetaInfo) {
		if meta.Basic != nil {
			for _, v := range *meta.Basic {
				s = append(s, v)
				if apiMeta == nil {
					params = append(params, strings.Join(s, "."))
				} else {
					pattern := strings.Join(s, ".")
					params = append(params, strings.Join(s, ".")+" "+apiMeta.GetReqTypeName(pattern))
				}
				onlyKepParams = append(onlyKepParams, strings.Join(s, "."))
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
