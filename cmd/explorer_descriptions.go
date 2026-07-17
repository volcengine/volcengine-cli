package cmd

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/volcengine/volcengine-cli/asset"
)

const explorerDescriptionsAsset = "volcengine-sdk-metadata/explorer_descriptions/descriptions.json"

type ServiceDescription struct {
	ServiceCn string `json:"service_cn,omitempty"`
	ServiceEn string `json:"service_en,omitempty"`
}

type ApiDescription struct {
	NameCn        string `json:"name_cn,omitempty"`
	NameEn        string `json:"name_en,omitempty"`
	DescriptionCn string `json:"description_cn,omitempty"`
	DescriptionEn string `json:"description_en,omitempty"`
	Description   string `json:"description,omitempty"`
}

type explorerDescriptionsData struct {
	Services map[string]ServiceDescription        `json:"services"`
	Apis     map[string]map[string]ApiDescription `json:"apis"`
}

var (
	explorerDescriptionsOnce      sync.Once
	explorerDescriptions          explorerDescriptionsData
	loadExplorerDescriptionsAsset = func() ([]byte, error) {
		return asset.Asset(explorerDescriptionsAsset)
	}
)

var fallbackServiceDescriptions = map[string]ServiceDescription{
	"sts": {
		ServiceCn: "安全令牌服务",
		ServiceEn: "Security Token Service for temporary credentials and caller identity.",
	},
	"ecs": {
		ServiceCn: "云服务器",
		ServiceEn: "Elastic Compute Service for virtual machines and compute resources.",
	},
	"vpc": {
		ServiceCn: "私有网络",
		ServiceEn: "Virtual Private Cloud networking, subnets, routes, and security resources.",
	},
	"clb": {
		ServiceCn: "负载均衡",
		ServiceEn: "Cloud Load Balancer for distributing traffic across backend servers.",
	},
	"rds_mysql": {
		ServiceCn: "云数据库 MySQL 版",
		ServiceEn: "Managed MySQL database instances, accounts, backups, and network access.",
	},
}

var fallbackApiDescriptions = map[string]map[string]ApiDescription{
	"sts": {
		"GetCallerIdentity": {
			NameCn:        "获取请求者身份信息",
			NameEn:        "Get identity information for the request credential",
			DescriptionEn: "Get identity information for the request credential",
		},
	},
	"clb": {
		"AttachHealthCheckLogTopic": {
			NameCn:        "绑定健康检查日志主题",
			DescriptionEn: "Binds a CLB instance to a health check log topic.",
		},
	},
	"rds_mysql": {
		"ModifyDBInstanceIPList": {
			NameCn:        "修改实例白名单",
			DescriptionEn: "Modifies an RDS MySQL instance IP allowlist.",
		},
		"ListDBInstanceIPLists": {
			NameCn:        "查询实例白名单",
			DescriptionEn: "Lists IP allowlists of an RDS MySQL instance.",
		},
	},
}

func loadExplorerDescriptions() explorerDescriptionsData {
	explorerDescriptionsOnce.Do(func() {
		explorerDescriptions = explorerDescriptionsData{
			Services: map[string]ServiceDescription{},
			Apis:     map[string]map[string]ApiDescription{},
		}
		data, err := loadExplorerDescriptionsAsset()
		if err != nil {
			return
		}
		var loaded explorerDescriptionsData
		if err := json.Unmarshal(data, &loaded); err != nil {
			return
		}
		if loaded.Services != nil {
			explorerDescriptions.Services = loaded.Services
		}
		if loaded.Apis != nil {
			explorerDescriptions.Apis = loaded.Apis
		}
	})
	return explorerDescriptions
}

func getServiceDescription(service string) ServiceDescription {
	if d, ok := lookupAssetServiceDescription(service); ok {
		if fallback, ok := fallbackServiceDescriptions[service]; ok {
			d = mergeServiceDescription(d, fallback, service)
		}
		if d.ServiceCn == "" {
			d.ServiceCn = "火山引擎 " + service + " 服务"
		}
		return d
	}
	if d, ok := fallbackServiceDescriptions[service]; ok {
		return d
	}
	return ServiceDescription{
		ServiceCn: "火山引擎 " + service + " 服务",
	}
}

func lookupAssetServiceDescription(service string) (ServiceDescription, bool) {
	descriptions := loadExplorerDescriptions()
	if d, ok := descriptions.Services[service]; ok {
		return d, true
	}
	if mapped, ok := GetServiceMapping(service); ok && mapped != service {
		if d, ok := descriptions.Services[mapped]; ok {
			return d, true
		}
	}
	return ServiceDescription{}, false
}

func formatServiceShort(service string) string {
	d := getServiceDescription(service)
	if currentLanguage == LanguageSimplifiedChinese {
		if strings.TrimSpace(d.ServiceCn) != "" {
			return d.ServiceCn
		}
		return "火山引擎 " + service + " 服务"
	}
	if strings.TrimSpace(d.ServiceEn) != "" && !containsCJK(d.ServiceEn) {
		return d.ServiceEn
	}
	return service
}

func getApiDescription(service, action string) ApiDescription {
	descriptions := loadExplorerDescriptions()
	fallback, hasFallback := fallbackApiDescriptions[service][action]
	if m, ok := descriptions.Apis[service]; ok {
		if d, ok := m[action]; ok {
			if hasFallback {
				return mergeApiDescription(d, fallback)
			}
			return d
		}
	}
	if mapped, ok := GetServiceMapping(service); ok && mapped != service {
		if m, ok := descriptions.Apis[mapped]; ok {
			if d, ok := m[action]; ok {
				if hasFallback {
					return mergeApiDescription(d, fallback)
				}
				return d
			}
		}
	}
	if hasFallback {
		return fallback
	}
	return ApiDescription{}
}

func mergeServiceDescription(primary, fallback ServiceDescription, service string) ServiceDescription {
	if primary.ServiceCn == "" {
		primary.ServiceCn = fallback.ServiceCn
	}
	if primary.ServiceEn == "" || strings.EqualFold(primary.ServiceEn, service) || len(primary.ServiceEn) <= 16 {
		primary.ServiceEn = fallback.ServiceEn
	}
	return primary
}

func mergeApiDescription(primary, fallback ApiDescription) ApiDescription {
	if primary.NameCn == "" {
		primary.NameCn = fallback.NameCn
	}
	if primary.NameEn == "" || containsCJK(primary.NameEn) {
		primary.NameEn = fallback.NameEn
	}
	if primary.DescriptionCn == "" {
		primary.DescriptionCn = fallback.DescriptionCn
	}
	if primary.DescriptionEn == "" || containsCJK(primary.DescriptionEn) {
		primary.DescriptionEn = fallback.DescriptionEn
	}
	return primary
}

func formatActionShort(service, action string) string {
	d := getApiDescription(service, action)
	if currentLanguage == LanguageSimplifiedChinese {
		if strings.TrimSpace(d.NameCn) != "" {
			return d.NameCn
		}
		return action
	}
	if strings.TrimSpace(d.NameEn) != "" && !containsCJK(d.NameEn) {
		return d.NameEn
	}
	return action
}

func formatActionLong(service, action string) string {
	d := getApiDescription(service, action)
	if currentLanguage == LanguageSimplifiedChinese {
		if strings.TrimSpace(d.DescriptionCn) != "" {
			return firstLine(d.DescriptionCn)
		}
		if strings.TrimSpace(d.Description) != "" && containsCJK(d.Description) {
			return firstLine(d.Description)
		}
		return formatActionShort(service, action)
	}
	if strings.TrimSpace(d.DescriptionEn) != "" && !containsCJK(d.DescriptionEn) {
		return firstLine(d.DescriptionEn)
	}
	if strings.TrimSpace(d.Description) != "" && !containsCJK(d.Description) {
		return firstLine(d.Description)
	}
	return formatActionShort(service, action)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "\n"); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}

func containsCJK(value string) bool {
	for _, r := range value {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}
