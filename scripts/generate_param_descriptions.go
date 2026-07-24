// Command generate_param_descriptions fetches OpenAPI parameter descriptions
// for every service/version present in the SDK metadata inventory, and writes
// a standalone JSON product consumed by build_asset.sh (go-bindata → asset).
//
// Inventory resolution (same idea as generate_explorer_descriptions.go):
//  1. --metadata-dir (default: <repo>/volcengine-sdk-metadata/metadata)
//  2. fallback: parse paths in asset/asset.go
//
// Data source (public Explorer APIs, no DB):
//  - GET /api/common/explorer/services        → canonical ServiceCode casing
//  - GET /api/common/explorer/apis            → action list per service/version
//  - GET /api/common/explorer/api-swagger     → param descriptions per action
//
// ServiceCode casing: metadata inventory dirs are lowercased, but api-swagger is
// case-sensitive (cdn≠CDN, acep≠ACEP, advdefence≠AdvDefence). Requests use the
// canonical code from /services; params.json keys stay inventory lower for CLI join.
//
// Rate limiting: serial requests with --delay (default 150ms). Retries on 429/5xx.
//
// Usage:
//
//	go run ./scripts/generate_param_descriptions.go
//	go run ./scripts/generate_param_descriptions.go --service ecs --version 2020-04-01
//	go run ./scripts/generate_param_descriptions.go --delay 200ms --lang zh
//
// Full asset rebuild (metadata + descriptions + params + bindata):
//
//	sh build_asset.sh <metadata-git-url> [branch]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	defaultServicesURL   = "https://api.volcengine.com/api/common/explorer/services"
	defaultApisURL       = "https://api.volcengine.com/api/common/explorer/apis"
	defaultAPISwaggerURL = "https://api.volcengine.com/api/common/explorer/api-swagger"
)

type paramDescription struct {
	DescriptionCn string `json:"description_cn,omitempty"`
	DescriptionEn string `json:"description_en,omitempty"`
	Required      bool   `json:"required,omitempty"`
}

// service -> version -> action -> paramName -> description
type paramsFile struct {
	Apis map[string]map[string]map[string]map[string]paramDescription `json:"apis"`
}

type servicesPayload struct {
	Result struct {
		Categories []struct {
			Services []struct {
				ServiceCode string `json:"ServiceCode"`
			} `json:"Services"`
		} `json:"Categories"`
	} `json:"Result"`
}

type apisPayload struct {
	Result struct {
		Groups []struct {
			Apis []struct {
				Action string `json:"Action"`
			} `json:"Apis"`
		} `json:"Groups"`
	} `json:"Result"`
}

type swaggerEnvelope struct {
	Result struct {
		Api json.RawMessage `json:"Api"`
	} `json:"Result"`
}

type openAPIDoc struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components *struct {
		Schemas map[string]*schemaNode `json:"schemas"`
	} `json:"components"`
}

type operationNode struct {
	Parameters  []parameterNode `json:"parameters"`
	RequestBody *requestBodyNode `json:"requestBody"`
}

type parameterNode struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Schema      *schemaNode `json:"schema"`
}

type requestBodyNode struct {
	Content map[string]struct {
		Schema *schemaNode `json:"schema"`
	} `json:"content"`
}

type schemaNode struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Required    []string               `json:"required"`
	Properties  map[string]*schemaNode `json:"properties"`
	Items       *schemaNode            `json:"items"`
	Ref         string                 `json:"$ref"`
}

var directiveRE = regexp.MustCompile(`:::[a-zA-Z]+\n?`)

func main() {
	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}

	metadataDir := flag.String("metadata-dir", filepath.Join(root, "volcengine-sdk-metadata", "metadata"), "metadata directory for service/version inventory")
	assetGo := flag.String("asset-go", filepath.Join(root, "asset", "asset.go"), "generated asset.go fallback for inventory")
	out := flag.String("out", filepath.Join(root, "volcengine-sdk-metadata", "param_descriptions", "params.json"), "output JSON path")
	delay := flag.Duration("delay", 150*time.Millisecond, "pause between HTTP calls (rate limit)")
	lang := flag.String("lang", "both", "language to fetch: zh | en | both")
	serviceFilter := flag.String("service", "", "only this service code (optional)")
	versionFilter := flag.String("version", "", "only this API version (optional)")
	maxActions := flag.Int("max-actions", 0, "stop after N swagger fetches (0 = no limit, useful for smoke tests)")
	flag.Parse()

	versions := loadVersions(*metadataDir)
	if len(versions) == 0 {
		fmt.Fprintf(os.Stderr, "metadata-dir empty or missing (%s), fallback to asset.go\n", *metadataDir)
		versions = loadVersionsFromBindata(*assetGo)
	}
	if len(versions) == 0 {
		fatal(fmt.Errorf("no service/version inventory found; set --metadata-dir to vestack-sdk-metadata/metadata (or volcengine-sdk-metadata/metadata), or ensure asset/asset.go exists"))
	}

	wantZH, wantEN := parseLang(*lang)
	file := paramsFile{Apis: map[string]map[string]map[string]map[string]paramDescription{}}

	// Canonical ServiceCode casing for Explorer HTTP (api-swagger is case-sensitive).
	codeMap, err := fetchServiceCodeMap(*delay)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: fetch /services for ServiceCode casing failed (%v); using inventory names as-is\n", err)
		codeMap = map[string]string{}
	} else {
		fmt.Fprintf(os.Stderr, "loaded %d canonical ServiceCodes from /services\n", len(codeMap))
	}

	services := sortedKeys(versions)
	totalSwagger := 0
	doneSwagger := 0 // successful actions with at least one param description
	skipped := 0

	for _, svc := range services {
		if *serviceFilter != "" && !strings.EqualFold(svc, *serviceFilter) {
			continue
		}
		explorerCode := resolveServiceCode(svc, codeMap)
		if explorerCode != svc {
			fmt.Fprintf(os.Stderr, "service code casing: inventory=%s explorer=%s\n", svc, explorerCode)
		}
		for _, ver := range versions[svc] {
			if *versionFilter != "" && ver != *versionFilter {
				continue
			}
			actions, err := fetchActionList(explorerCode, ver, *delay)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skip action list %s@%s (explorer=%s): %v\n", svc, ver, explorerCode, err)
				skipped++
				continue
			}
			if len(actions) == 0 {
				continue
			}
			if _, ok := file.Apis[svc]; !ok {
				file.Apis[svc] = map[string]map[string]map[string]paramDescription{}
			}
			if _, ok := file.Apis[svc][ver]; !ok {
				file.Apis[svc][ver] = map[string]map[string]paramDescription{}
			}

			for _, action := range actions {
				if *maxActions > 0 && totalSwagger >= *maxActions {
					fmt.Fprintf(os.Stderr, "reached --max-actions=%d, stopping early\n", *maxActions)
					goto write
				}
				totalSwagger++
				params := map[string]paramDescription{}

				if wantZH {
					zh, err := fetchParamDescriptions(explorerCode, ver, action, "zh", *delay)
					if err != nil {
						fmt.Fprintf(os.Stderr, "skip swagger zh %s@%s %s (explorer=%s): %v\n", svc, ver, action, explorerCode, err)
						skipped++
					} else {
						mergeParamLang(params, zh, "zh")
					}
				}
				if wantEN {
					en, err := fetchParamDescriptions(explorerCode, ver, action, "en", *delay)
					if err != nil {
						fmt.Fprintf(os.Stderr, "skip swagger en %s@%s %s (explorer=%s): %v\n", svc, ver, action, explorerCode, err)
						skipped++
					} else {
						mergeParamLang(params, en, "en")
					}
				}

				if len(params) > 0 {
					file.Apis[svc][ver][action] = params
					doneSwagger++
					if doneSwagger%50 == 0 || doneSwagger == 1 {
						fmt.Fprintf(os.Stderr, "progress swagger fetches: %d (last %s@%s %s explorer=%s)\n", doneSwagger, svc, ver, action, explorerCode)
					}
				}
			}
		}
	}

write:
	if err := writeJSON(*out, file); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (swagger_ok=%d, attempted≈%d, skipped_errors=%d)\n", *out, doneSwagger, totalSwagger, skipped)

	// Fail closed when inventory existed but nothing was produced (e.g. total network failure).
	// Smoke tests with --max-actions still write partial products and may have doneSwagger > 0.
	if doneSwagger == 0 && skipped > 0 {
		fatal(fmt.Errorf("no swagger fetches succeeded (%d errors); refusing empty/sparse product", skipped))
	}
}

func parseLang(lang string) (zh, en bool) {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "cn", "zh-cn":
		return true, false
	case "en", "en-us":
		return false, true
	default:
		return true, true
	}
}

func mergeParamLang(dst map[string]paramDescription, src map[string]paramDescription, lang string) {
	for name, p := range src {
		cur := dst[name]
		switch lang {
		case "en":
			if p.DescriptionEn != "" {
				cur.DescriptionEn = p.DescriptionEn
			}
		default:
			if p.DescriptionCn != "" {
				cur.DescriptionCn = p.DescriptionCn
			}
		}
		if p.Required {
			cur.Required = true
		}
		dst[name] = cur
	}
}

// fetchServiceCodeMap returns lower(ServiceCode) → canonical ServiceCode from Explorer.
// api-swagger requires the exact casing published by /services.
func fetchServiceCodeMap(delay time.Duration) (map[string]string, error) {
	var payload servicesPayload
	if err := fetchJSONRetry(defaultServicesURL, "", &payload, delay); err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, cat := range payload.Result.Categories {
		for _, item := range cat.Services {
			code := strings.TrimSpace(item.ServiceCode)
			if code == "" {
				continue
			}
			key := strings.ToLower(code)
			// Prefer first-seen canonical form; Explorer publishes one official casing.
			if _, exists := out[key]; !exists {
				out[key] = code
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty services list from %s", defaultServicesURL)
	}
	return out, nil
}

// resolveServiceCode maps an inventory service name (usually lowercased metadata dir)
// to the Explorer-canonical ServiceCode for HTTP. Falls back to inventory name.
func resolveServiceCode(inventory string, codeMap map[string]string) string {
	if codeMap == nil {
		return inventory
	}
	if canon, ok := codeMap[strings.ToLower(strings.TrimSpace(inventory))]; ok && canon != "" {
		return canon
	}
	return inventory
}

func fetchActionList(service, version string, delay time.Duration) ([]string, error) {
	values := url.Values{}
	values.Set("ServiceCode", service)
	values.Set("APIVersion", version)
	endpoint := defaultApisURL + "?" + values.Encode()

	var payload apisPayload
	if err := fetchJSONRetry(endpoint, "zh", &payload, delay); err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	for _, g := range payload.Result.Groups {
		for _, a := range g.Apis {
			action := strings.TrimSpace(a.Action)
			if action != "" {
				set[action] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Strings(out)
	return out, nil
}

func fetchParamDescriptions(service, version, action, language string, delay time.Duration) (map[string]paramDescription, error) {
	values := url.Values{}
	values.Set("ServiceCode", service)
	values.Set("Version", version)
	values.Set("APIVersion", version)
	values.Set("ActionName", action)
	endpoint := defaultAPISwaggerURL + "?" + values.Encode()

	langHeader := language
	if language == "zh" {
		langHeader = "" // default Chinese on the public site
	}

	var env swaggerEnvelope
	if err := fetchJSONRetry(endpoint, langHeader, &env, delay); err != nil {
		return nil, err
	}
	if len(env.Result.Api) == 0 {
		return map[string]paramDescription{}, nil
	}

	var doc openAPIDoc
	if err := json.Unmarshal(env.Result.Api, &doc); err != nil {
		return nil, err
	}
	return extractParamsFromOpenAPI(&doc, language), nil
}

func extractParamsFromOpenAPI(doc *openAPIDoc, language string) map[string]paramDescription {
	out := map[string]paramDescription{}
	var schemas map[string]*schemaNode
	if doc.Components != nil {
		schemas = doc.Components.Schemas
	}

	for _, methods := range doc.Paths {
		for _, raw := range methods {
			var op operationNode
			if err := json.Unmarshal(raw, &op); err != nil {
				continue
			}
			for _, p := range op.Parameters {
				name := strings.TrimSpace(p.Name)
				if name == "" {
					continue
				}
				desc := strings.TrimSpace(p.Description)
				if desc == "" && p.Schema != nil {
					desc = strings.TrimSpace(p.Schema.Description)
				}
				desc = sanitizeDescription(desc)
				if desc == "" && !p.Required {
					// still record required-only params without text
				}
				putParam(out, name, desc, p.Required, language)
			}
			if op.RequestBody != nil {
				for _, c := range op.RequestBody.Content {
					if c.Schema == nil {
						continue
					}
					collectSchemaParams("", c.Schema, schemas, out, language)
				}
			}
		}
	}
	return out
}

func collectSchemaParams(prefix string, s *schemaNode, schemas map[string]*schemaNode, out map[string]paramDescription, language string) {
	s = resolveSchema(s, schemas)
	if s == nil {
		return
	}
	reqSet := map[string]bool{}
	for _, r := range s.Required {
		reqSet[r] = true
	}
	for name, prop := range s.Properties {
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		prop = resolveSchema(prop, schemas)
		desc := ""
		required := reqSet[name]
		if prop != nil {
			desc = sanitizeDescription(prop.Description)
			// nested object
			if len(prop.Properties) > 0 || (prop.Items != nil) {
				collectSchemaParams(key, prop, schemas, out, language)
			}
		}
		putParam(out, key, desc, required, language)
	}
	if s.Items != nil && (len(s.Properties) == 0) {
		// array of objects: expose item fields under prefix
		collectSchemaParams(prefix, s.Items, schemas, out, language)
	}
}

func resolveSchema(s *schemaNode, schemas map[string]*schemaNode) *schemaNode {
	if s == nil {
		return nil
	}
	if s.Ref == "" || schemas == nil {
		return s
	}
	// #/components/schemas/Foo
	parts := strings.Split(s.Ref, "/")
	name := parts[len(parts)-1]
	if ref, ok := schemas[name]; ok {
		return ref
	}
	return s
}

func putParam(out map[string]paramDescription, name, desc string, required bool, language string) {
	cur := out[name]
	if required {
		cur.Required = true
	}
	switch language {
	case "en":
		if desc != "" {
			cur.DescriptionEn = desc
		}
	default:
		if desc != "" {
			cur.DescriptionCn = desc
		}
	}
	// Keep entry even if description empty but required, so callers know the param exists.
	if cur.DescriptionCn != "" || cur.DescriptionEn != "" || cur.Required {
		out[name] = cur
	}
}

func fetchJSONRetry(endpoint, language string, out interface{}, delay time.Duration) error {
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			// backoff: 1s, 2s, 4s
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
		} else if delay > 0 {
			time.Sleep(delay)
		}
		last = fetchJSON(endpoint, language, out)
		if last == nil {
			return nil
		}
		// retry only transient failures
		msg := last.Error()
		if strings.Contains(msg, "429") || strings.Contains(msg, "502") ||
			strings.Contains(msg, "503") || strings.Contains(msg, "504") ||
			strings.Contains(msg, "timeout") || strings.Contains(msg, "Timeout") ||
			strings.Contains(msg, "connection") {
			continue
		}
		return last
	}
	return last
}

func fetchJSON(endpoint, language string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if language != "" {
		req.Header.Set("x-language", language)
	}
	req.Header.Set("accept", "application/json, text/plain, */*")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(data)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return fmt.Errorf("GET %s returned %s: %s", endpoint, resp.Status, snippet)
	}
	return json.Unmarshal(data, out)
}

func sanitizeDescription(value string) string {
	value = directiveRE.ReplaceAllString(value, "")
	value = strings.Replace(value, ":::", "", -1)
	return strings.TrimSpace(value)
}

func writeJSON(path string, file paramsFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return ioutil.WriteFile(path, data, 0644)
}

func loadVersions(base string) map[string][]string {
	versions := make(map[string][]string)
	entries, err := ioutil.ReadDir(base)
	if err != nil {
		return versions
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		serviceDir := filepath.Join(base, entry.Name())
		versionEntries, err := ioutil.ReadDir(serviceDir)
		if err != nil {
			continue
		}
		for _, versionEntry := range versionEntries {
			if versionEntry.IsDir() {
				service := strings.ToLower(entry.Name())
				versions[service] = append(versions[service], versionEntry.Name())
			}
		}
	}
	for svc := range versions {
		sort.Strings(versions[svc])
	}
	return versions
}

func loadVersionsFromBindata(assetGo string) map[string][]string {
	versions := make(map[string]map[string]struct{})
	data, err := ioutil.ReadFile(assetGo)
	if err != nil {
		return map[string][]string{}
	}
	pattern := regexp.MustCompile(`volcengine-sdk-metadata/metadata/([^/]+)/([^/]+)/metadata\.json`)
	matches := pattern.FindAllStringSubmatch(string(data), -1)
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		service := strings.ToLower(match[1])
		if versions[service] == nil {
			versions[service] = make(map[string]struct{})
		}
		versions[service][match[2]] = struct{}{}
	}

	out := make(map[string][]string)
	for service, set := range versions {
		for version := range set {
			out[service] = append(out[service], version)
		}
		sort.Strings(out[service])
	}
	return out
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd, nil
		}
		next := filepath.Dir(wd)
		if next == wd {
			return "", fmt.Errorf("go.mod not found from %s", wd)
		}
		wd = next
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
