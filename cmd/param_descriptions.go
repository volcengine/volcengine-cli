package cmd

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/volcengine/volcengine-cli/asset/paramdescriptions"
)

// paramDescriptionsAsset is the bindata key for asset/paramdescriptions/params.json
// (source kept next to bindata.go; regenerate with go generate ./asset/paramdescriptions).
// Kept separate from asset/asset.go so metadata/explorer rebuilds do not wipe
// a ~6MB param corpus, and param-only refreshes stay reviewable.
const paramDescriptionsAsset = "params.json"

type paramDescription struct {
	DescriptionCn string `json:"description_cn,omitempty"`
	DescriptionEn string `json:"description_en,omitempty"`
	ExampleCn     string `json:"example_cn,omitempty"`
	ExampleEn     string `json:"example_en,omitempty"`
	// Required is a pointer so omitempty-missing fields do not look like explicit false
	// and downgrade SDK-metadata required=true when attaching help text.
	Required *bool `json:"required,omitempty"`
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
		return paramdescriptions.Asset(paramDescriptionsAsset)
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

// attachParamDescriptions fills p.description, p.example and p.required from the
// CAE asset for help display. Missing asset / keys leave description/example empty
// and keep the required flag already set from SDK metadata (if any).
// required is only overwritten when the asset explicitly sets the field (true or false);
// a missing required key never downgrades SDK-metadata required=true to optional.
func attachParamDescriptions(service, action string, params []param) {
	if len(params) == 0 {
		return
	}
	version := ""
	if rootSupport != nil {
		version = rootSupport.GetVersion(service)
	}
	for i := range params {
		info, ok := lookupParamDescriptionInfo(service, version, action, params[i].key)
		if !ok {
			continue
		}
		params[i].description = info.text
		params[i].example = info.example
		if info.requiredPresent {
			params[i].required = info.required
		}
	}
}

type paramDescriptionInfo struct {
	text            string
	example         string
	required        bool
	requiredPresent bool
}

// lookupParamDescription resolves text for one parameter under the current language.
func lookupParamDescription(service, version, action, paramKey string) string {
	info, ok := lookupParamDescriptionInfo(service, version, action, paramKey)
	if !ok {
		return ""
	}
	return info.text
}

// lookupParamDescriptionInfo resolves description text and required for one parameter.
func lookupParamDescriptionInfo(service, version, action, paramKey string) (paramDescriptionInfo, bool) {
	d := loadParamDescriptions()
	if len(d.Apis) == 0 {
		return paramDescriptionInfo{}, false
	}
	action = strings.TrimSpace(action)
	if action == "" || strings.TrimSpace(paramKey) == "" {
		return paramDescriptionInfo{}, false
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
			if info, found := paramDescriptionInfoForKey(params, paramKey); found {
				return info, true
			}
		}
	}
	return paramDescriptionInfo{}, false
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

// paramVersionCandidates returns only the exact preferred API version when that
// version has the action in the asset. Never falls back to another version's
// text (wrong-version help is worse than empty help).
func paramVersionCandidates(verMap map[string]map[string]map[string]paramDescription, preferred, action string) []string {
	preferred = strings.TrimSpace(preferred)
	action = strings.TrimSpace(action)
	if preferred == "" || action == "" || verMap == nil {
		return nil
	}
	actionMap, ok := verMap[preferred]
	if !ok {
		return nil
	}
	if _, ok := actionMap[action]; !ok {
		return nil
	}
	return []string{preferred}
}

func paramDescriptionInfoForKey(params map[string]paramDescription, paramKey string) (paramDescriptionInfo, bool) {
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
			info := paramDescriptionInfo{
				text:    pickParamDescription(p),
				example: pickParamExample(p),
			}
			if p.Required != nil {
				info.required = *p.Required
				info.requiredPresent = true
			}
			return info, true
		}
	}
	return paramDescriptionInfo{}, false
}

func pickParamDescription(p paramDescription) string {
	// Full multi-line text is preserved for -h (formatParamsHelpUsage wraps lines).
	// Do not fall back across languages: missing EN stays empty under EN (and vice versa),
	// so help never mixes Chinese into English mode when CAE only has one language.
	if currentLanguage == LanguageSimplifiedChinese {
		return strings.TrimSpace(p.DescriptionCn)
	}
	return strings.TrimSpace(p.DescriptionEn)
}

func pickParamExample(p paramDescription) string {
	// Same language isolation as descriptions.
	if currentLanguage == LanguageSimplifiedChinese {
		return strings.TrimSpace(p.ExampleCn)
	}
	return strings.TrimSpace(p.ExampleEn)
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
