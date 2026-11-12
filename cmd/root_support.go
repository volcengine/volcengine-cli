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

	//generate structure info form meta and set a map with service_version:pkgName
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
			//if structure info is nil skip it
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
			//if api metadata is nil skip it, otherwise the service would be registered without a name
			if svcName, ok := svcs[spaces[2]+"_"+spaces[3]]; ok && svcName != "" {
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

// GetVisibleSvcCompatible 返回默认发现可见的服务写法（含兼容别名），过滤已废弃
// 服务的默认入口。完整可调用集合仍使用 GetAllSvcCompatible，显式旧版调用不受影响。
func (r *RootSupport) GetVisibleSvcCompatible() []string {
	all := r.GetAllSvcCompatible()
	visible := make([]string, 0, len(all))
	for _, svc := range all {
		if isDefaultHiddenService(svc) {
			continue
		}
		visible = append(visible, svc)
	}
	return visible
}

// GetVisibleSvc 返回默认发现可见的规范服务列表，用于 MCP list_services 等发现路径。
func (r *RootSupport) GetVisibleSvc() []string {
	visible := make([]string, 0, len(r.SupportSvc))
	for _, svc := range r.SupportSvc {
		if isDefaultHiddenService(svc) {
			continue
		}
		visible = append(visible, svc)
	}
	return visible
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

func (r *RootSupport) GetApiMeta(svc string, action string) *ApiMeta {
	if metas, ok := r.SupportTypes[svc]; ok {
		return metas[action]
	}
	return nil
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
