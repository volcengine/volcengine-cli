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
	defaultServicesURL = "https://api.volcengine.com/api/common/explorer/services"
	defaultApisURL     = "https://api.volcengine.com/api/common/explorer/apis"
)

type serviceDescription struct {
	ServiceCn string `json:"service_cn,omitempty"`
	ServiceEn string `json:"service_en,omitempty"`
}

type apiDescription struct {
	NameCn        string `json:"name_cn,omitempty"`
	NameEn        string `json:"name_en,omitempty"`
	DescriptionCn string `json:"description_cn,omitempty"`
	DescriptionEn string `json:"description_en,omitempty"`
}

type descriptionsFile struct {
	Services map[string]serviceDescription        `json:"services"`
	Apis     map[string]map[string]apiDescription `json:"apis"`
}

type servicesPayload struct {
	Result struct {
		Categories []struct {
			Services []serviceItem `json:"Services"`
		} `json:"Categories"`
	} `json:"Result"`
}

type serviceItem struct {
	ServiceCode string `json:"ServiceCode"`
	ServiceCn   string `json:"ServiceCn"`
	Product     string `json:"Product"`
}

type apisPayload struct {
	Result struct {
		Groups []struct {
			Apis []apiItem `json:"Apis"`
		} `json:"Groups"`
	} `json:"Result"`
}

type apiItem struct {
	Action      string `json:"Action"`
	NameCn      string `json:"NameCn"`
	NameEn      string `json:"NameEn"`
	Description string `json:"Description"`
}

var directiveRE = regexp.MustCompile(`:::[a-zA-Z]+\n?`)

func main() {
	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}

	metadataDir := flag.String("metadata-dir", filepath.Join(root, "volcengine-sdk-metadata", "metadata"), "metadata directory")
	assetGo := flag.String("asset-go", filepath.Join(root, "asset", "asset.go"), "generated asset.go fallback")
	out := flag.String("out", filepath.Join(root, "volcengine-sdk-metadata", "explorer_descriptions", "descriptions.json"), "output JSON path")
	flag.Parse()

	versions := loadVersions(*metadataDir)
	if len(versions) == 0 {
		versions = loadVersionsFromBindata(*assetGo)
	}

	services, err := fetchServices()
	if err != nil {
		fatal(err)
	}

	apiDescriptions := make(map[string]map[string]apiDescription)
	codes := sortedServiceCodes(services)
	for _, code := range codes {
		version := latestVersion(versions[code])
		if version == "" {
			continue
		}
		descriptions, err := fetchAPIDescriptions(code, version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip api descriptions for %s: %v\n", code, err)
			continue
		}
		if len(descriptions) > 0 {
			apiDescriptions[code] = descriptions
		}
		time.Sleep(50 * time.Millisecond)
	}

	file := descriptionsFile{
		Services: services,
		Apis:     apiDescriptions,
	}
	if err := writeJSON(*out, file); err != nil {
		fatal(err)
	}
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

func fetchServices() (map[string]serviceDescription, error) {
	var payloadCN servicesPayload
	if err := fetchJSON(defaultServicesURL, "", &payloadCN); err != nil {
		return nil, err
	}
	var payloadEN servicesPayload
	if err := fetchJSON(defaultServicesURL, "en", &payloadEN); err != nil {
		return nil, err
	}

	servicesEN := indexServices(payloadEN)
	services := make(map[string]serviceDescription)
	for _, category := range payloadCN.Result.Categories {
		for _, item := range category.Services {
			code := strings.ToLower(strings.TrimSpace(item.ServiceCode))
			if code == "" {
				continue
			}
			itemEN := servicesEN[code]
			serviceEN := firstNonEmpty(itemEN.ServiceCn, itemEN.Product)
			if isCodeLike(serviceEN, code) {
				serviceEN = ""
			}
			services[code] = serviceDescription{
				ServiceCn: item.ServiceCn,
				ServiceEn: serviceEN,
			}
		}
	}
	return services, nil
}

func fetchAPIDescriptions(code, version string) (map[string]apiDescription, error) {
	values := url.Values{}
	values.Set("ServiceCode", code)
	values.Set("APIVersion", version)
	endpoint := defaultApisURL + "?" + values.Encode()

	var payloadCN apisPayload
	if err := fetchJSON(endpoint, "", &payloadCN); err != nil {
		return nil, err
	}
	var payloadEN apisPayload
	if err := fetchJSON(endpoint, "en", &payloadEN); err != nil {
		return nil, err
	}

	actions := collectAPIDescriptions(payloadCN, "cn")
	actionsEN := collectAPIDescriptions(payloadEN, "en")
	for action, item := range actionsEN {
		target := actions[action]
		if target.NameEn == "" {
			target.NameEn = item.NameEn
		}
		if item.DescriptionEn != "" && !isLikelyChinese(item.DescriptionEn) {
			target.DescriptionEn = item.DescriptionEn
		}
		actions[action] = target
	}
	return actions, nil
}

func fetchJSON(endpoint, language string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if language != "" {
		req.Header.Set("x-language", language)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s returned %s", endpoint, resp.Status)
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func indexServices(payload servicesPayload) map[string]serviceItem {
	out := make(map[string]serviceItem)
	for _, category := range payload.Result.Categories {
		for _, item := range category.Services {
			code := strings.ToLower(strings.TrimSpace(item.ServiceCode))
			if code != "" {
				out[code] = item
			}
		}
	}
	return out
}

func collectAPIDescriptions(payload apisPayload, language string) map[string]apiDescription {
	actions := make(map[string]apiDescription)
	for _, group := range payload.Result.Groups {
		for _, api := range group.Apis {
			action := strings.TrimSpace(api.Action)
			if action == "" {
				continue
			}
			item := apiDescription{}
			switch language {
			case "en":
				if api.NameEn != "" && !isLikelyChinese(api.NameEn) {
					item.NameEn = api.NameEn
				} else if api.NameCn != "" && !isLikelyChinese(api.NameCn) {
					item.NameEn = api.NameCn
				}
				item.DescriptionEn = sanitizeDescription(api.Description)
			default:
				item.NameCn = api.NameCn
				item.DescriptionCn = sanitizeDescription(api.Description)
			}
			actions[action] = item
		}
	}
	return actions
}

func sanitizeDescription(value string) string {
	value = directiveRE.ReplaceAllString(value, "")
	value = strings.Replace(value, ":::", "", -1)
	return strings.TrimSpace(value)
}

func writeJSON(path string, file descriptionsFile) error {
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

func latestVersion(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	copied := append([]string{}, candidates...)
	sort.Strings(copied)
	return copied[len(copied)-1]
}

func sortedServiceCodes(services map[string]serviceDescription) []string {
	codes := make([]string, 0, len(services))
	for code := range services {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isCodeLike(value, code string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	return normalizeCodeLike(value) == normalizeCodeLike(code)
}

func normalizeCodeLike(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isLikelyChinese(value string) bool {
	for _, r := range value {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
