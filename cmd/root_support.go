package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/volcengine/volcengine-cli/asset"
	"github.com/volcengine/volcengine-cli/structset"
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
	svcs := make(map[string]string)
	//existSvcs := make(map[string]int)

	svcMappings := make(map[string]string)
	structSet := structset.AssetNames()
	sort.Strings(structSet)
	for _, name := range structSet {
		spaces := strings.Split(name, "/")
		b, _ := structset.Asset(name)
		st := StructInfo{}
		err := json.Unmarshal(b, &st)
		if err != nil {
			panic(err)
		}
		svcName := spaces[2]
		svcVersion := spaces[3]
		pkgName := st.PkgName
		svcMappings[svcName+"_"+svcVersion] = pkgName
		SetServiceMapping(pkgName, svcName)
	}

	temp := asset.AssetNames()
	sort.Strings(temp)
	for _, name := range temp {
		spaces := strings.Split(name, "/")
		if len(spaces) == 5 {
			var svcName string
			if s, ok := svcMappings[spaces[2]+"_"+spaces[3]]; ok {
				svcName = s
				svcs[spaces[2]+"_"+spaces[3]] = svcName
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
	}
	for _, name := range typeset.AssetNames() {
		spaces := strings.Split(name, "/")
		if len(spaces) == 5 {
			if _, ok := svcMappings[spaces[2]+"_"+spaces[3]]; ok {
				svcName := svcs[spaces[2]+"_"+spaces[3]]
				svc = append(svc, svcName)
				b, _ := typeset.Asset(name)
				meta := make(map[string]*ApiMeta)
				err := json.Unmarshal(b, &meta)
				if err != nil {
					panic(err)
				}
				types[svcName] = meta
			}
		}
	}

	return &RootSupport{
		SupportSvc:    svc,
		SupportAction: action,
		Versions:      version,
		SupportTypes:  types,
	}
}

func (r *RootSupport) GetAllSvcCompatible() []string {
	re := r.SupportSvc
	for _, v := range compatible_support_cmd {
		re = append(re, v)
	}
	return re
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
