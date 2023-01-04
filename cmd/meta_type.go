package cmd

import "strings"

// Copyright 2023 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

type MetaType struct {
	TypeName string `json:"TypeName,omitempty"`
	TypeOf   string `json:"TypeOf,omitempty"`
}

type Meta struct {
	MetaTypes  map[string]*MetaType `json:"MetaTypes,omitempty"`
	ChildMetas map[string]*Meta     `json:"ChildMetas,omitempty"`
}

type ApiMeta struct {
	Request  *Meta
	Response *Meta
}

func (m *ApiMeta) GetReqTypeName(pattern string) string {
	p := strings.Split(pattern, ".")
	var result string
	meta := m.Request

	if v, ok := meta.MetaTypes[pattern]; ok {
		return v.TypeName
	}

	var index int
	for _, _p := range p {
		index++
		metaTypes := meta.MetaTypes
		if _, ok := metaTypes[_p]; ok {
			result = " " + metaTypes[_p].TypeName
		} else {
			result = ""
		}
		if index < len(p) {
			if _, ok := meta.ChildMetas[_p]; ok {
				meta = meta.ChildMetas[_p]
			} else {
				break
			}
		}
	}
	return result
}
