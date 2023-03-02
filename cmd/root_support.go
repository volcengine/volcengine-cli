package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"encoding/json"
	"strings"

	"github.com/volcengine/volcengine-cli/asset"
	"github.com/volcengine/volcengine-cli/typeset"
)

type RootSupport struct {
	SupportSvc    []string
	SupportAction map[string]map[string]*VolcengineMeta
	Versions      map[string]string
	SupportTypes  map[string]map[string]*ApiMeta
}

func NewRootSupport() *RootSupport {
	var svc []string
	action := make(map[string]map[string]*VolcengineMeta)
	version := make(map[string]string)
	types := make(map[string]map[string]*ApiMeta)
	for _, name := range asset.AssetNames() {
		spaces := strings.Split(name, "/")
		if len(spaces) == 5 {
			svcName := spaces[2]
			if mappingSvc, ok := GetSvcVersionMapping(spaces[2], spaces[3]); ok {
				svcName = mappingSvc
			}
			svc = append(svc, svcName)
			b, _ := asset.Asset(name)
			action[svcName] = make(map[string]*VolcengineMeta)
			meta := make(map[string]*VolcengineMeta)
			err := json.Unmarshal(b, &meta)
			if err != nil {
				panic(err)
			}
			action[svcName] = meta
			version[svcName] = spaces[3]
		}
	}
	for _, name := range typeset.AssetNames() {
		spaces := strings.Split(name, "/")
		if len(spaces) == 5 {
			svcName := spaces[2]
			if mappingSvc, ok := GetSvcVersionMapping(spaces[2], spaces[3]); ok {
				svcName = mappingSvc
			}
			svc = append(svc, svcName)
			b, _ := typeset.Asset(name)
			types[svcName] = make(map[string]*ApiMeta)
			meta := make(map[string]*ApiMeta)
			err := json.Unmarshal(b, &meta)
			if err != nil {
				panic(err)
			}
			types[svcName] = meta
		}
	}

	return &RootSupport{
		SupportSvc:    svc,
		SupportAction: action,
		Versions:      version,
		SupportTypes:  types,
	}
}

func (r *RootSupport) GetAllSvc() []string {
	return r.SupportSvc
}

func (r *RootSupport) GetAllAction(svc string) []string {
	var as []string
	for k, _ := range r.SupportAction[svc] {
		as = append(as, k)
	}
	return as
}

func (r *RootSupport) GetVersion(svc string) string {
	return r.Versions[svc]
}

func (r *RootSupport) GetApiInfo(svc string, action string) *ApiInfo {
	for k, v := range r.SupportAction {
		if k == svc {
			if v1, ok := v[action]; ok {
				return v1.ApiInfo
			}
		}
	}
	return nil
}

func (r *RootSupport) IsValidSvc(svc string) bool {
	for _, s := range r.GetAllSvc() {
		if s == svc {
			return true
		}
	}
	return false
}

func (r *RootSupport) IsValidAction(svc, action string) bool {
	for k, v := range r.SupportAction {
		if k == svc {
			if _, ok := v[action]; ok {
				return true
			}
		}
	}
	return false
}
