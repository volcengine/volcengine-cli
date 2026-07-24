package cmd

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/volcengine/volcengine-cli/asset"
)

// paramDescriptionsAsset is produced by scripts/generate_param_descriptions.go
// and embedded via build_asset.sh (go-bindata → package asset).
const paramDescriptionsAsset = "volcengine-sdk-metadata/param_descriptions/params.json"

type paramDescription struct {
	DescriptionCn string `json:"description_cn,omitempty"`
	DescriptionEn string `json:"description_en,omitempty"`
	Required      bool   `json:"required,omitempty"`
}

// paramDescriptionsData mirrors scripts/generate_param_descriptions.go output:
// apis[service][version][action][paramName] → description.
type paramDescriptionsData struct {
	Apis map[string]map[string]map[string]map[string]paramDescription `json:"apis"`
}

var (
	paramDescriptionsOnce      sync.Once
	paramDescriptions          paramDescriptionsData
	loadParamDescriptionsAsset = func() ([]byte, error) {
		return asset.Asset(paramDescriptionsAsset)
	}
)

func loadParamDescriptions() paramDescriptionsData {
	paramDescriptionsOnce.Do(func() {
		paramDescriptions = paramDescriptionsData{
			Apis: map[string]map[string]map[string]map[string]paramDescription{},
		}
		data, err := loadParamDescriptionsAsset()
		if err != nil {
			return
		}
		var loaded paramDescriptionsData
		if err := json.Unmarshal(data, &loaded); err != nil {
			return
		}
		if loaded.Apis != nil {
			paramDescriptions.Apis = loaded.Apis
		}
	})
	return paramDescriptions
}

// attachParamDescriptions fills p.description for help display.
// Missing asset / keys leave description empty (same layout as before).
func attachParamDescriptions(service, action string, params []param) {
	if len(params) == 0 {
		return
	}
	version := ""
	if rootSupport != nil {
		version = rootSupport.GetVersion(service)
	}
	for i := range params {
		params[i].description = lookupParamDescription(service, version, action, params[i].key)
	}
}

// lookupParamDescription resolves text for one parameter under the current language.
func lookupParamDescription(service, version, action, paramKey string) string {
	d := loadParamDescriptions()
	if len(d.Apis) == 0 {
		return ""
	}
	action = strings.TrimSpace(action)
	if action == "" || strings.TrimSpace(paramKey) == "" {
		return ""
	}

	for _, svc := range paramServiceCandidates(service) {
		verMap, ok := d.Apis[svc]
		if !ok || len(verMap) == 0 {
			continue
		}
		versions := paramVersionCandidates(verMap, version, action)
		for _, ver := range versions {
			actionMap, ok := verMap[ver]
			if !ok {
				continue
			}
			params, ok := actionMap[action]
			if !ok || len(params) == 0 {
				continue
			}
			if text := paramDescriptionText(params, paramKey); text != "" {
				return text
			}
		}
	}
	return ""
}

func paramServiceCandidates(service string) []string {
	service = strings.TrimSpace(service)
	if service == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(service)
	if mapped, ok := GetServiceMapping(service); ok && mapped != "" {
		add(mapped)
		add(strings.ToLower(mapped))
	}
	// Generator inventory keys are lowercased metadata folder names.
	add(strings.ToLower(service))
	return out
}

// paramVersionCandidates prefers the CLI default version, then any version that
// contains the action (lexicographically latest first for stability).
func paramVersionCandidates(verMap map[string]map[string]map[string]paramDescription, preferred, action string) []string {
	if preferred != "" {
		if actionMap, ok := verMap[preferred]; ok {
			if _, ok := actionMap[action]; ok {
				return []string{preferred}
			}
		}
	}
	var found []string
	for ver, actionMap := range verMap {
		if _, ok := actionMap[action]; ok {
			found = append(found, ver)
		}
	}
	if len(found) == 0 {
		return nil
	}
	// Prefer newer-looking ISO date strings first.
	sort.Sort(sort.Reverse(sort.StringSlice(found)))
	return found
}

func paramDescriptionText(params map[string]paramDescription, paramKey string) string {
	// Try exact key, then metatype-style .N placeholders, then strip indices entirely
	// (swagger/OpenAPI often uses Tags.Key while help lists Tags.1.Key or Tags.N.Key).
	keys := []string{
		paramKey,
		normalizeParamDescKeyKeepN(paramKey),
		normalizeParamDescKey(paramKey),
	}
	seen := map[string]struct{}{}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		if p, ok := params[k]; ok {
			if text := pickParamDescription(p); text != "" {
				return text
			}
		}
	}
	return ""
}

func pickParamDescription(p paramDescription) string {
	if currentLanguage == LanguageSimplifiedChinese {
		if t := firstLine(p.DescriptionCn); t != "" {
			return t
		}
		return firstLine(p.DescriptionEn)
	}
	if t := firstLine(p.DescriptionEn); t != "" {
		return t
	}
	return firstLine(p.DescriptionCn)
}

// normalizeParamDescKey strips pure-numeric path segments and "N" placeholders
// so help keys like NetworkInterfaces.1.SubnetId / Tags.N.Key match swagger
// keys NetworkInterfaces.SubnetId / Tags.Key.
func normalizeParamDescKey(key string) string {
	parts := strings.Split(key, ".")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		if isParamIndexSegment(p) {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, ".")
}

// normalizeParamDescKeyKeepN rewrites numeric indices to N but keeps N segments,
// matching MetaTypes keys such as Tags.N.Key.
func normalizeParamDescKeyKeepN(key string) string {
	parts := strings.Split(key, ".")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		if isAllDigits(p) {
			out = append(out, "N")
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, ".")
}

func isParamIndexSegment(s string) bool {
	return s == "N" || s == "n" || isAllDigits(s)
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
