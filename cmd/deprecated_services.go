// deprecated_services.go 在非生成源码中维护本期服务版本废弃记录。
//
// 记录只用于：
//   - 默认入口可见性（根帮助、补全候选、MCP list_services 等发现路径）；
//   - 显式旧版调用的迁移提示。
//
// 不修改内嵌 metadata/structure/metatype/bindata，不移除任何命令注册，
// 不改写请求服务名、版本、参数和返回值。
package cmd

import (
	"fmt"
	"os"
	"strings"
)

// deprecatedService 描述一条「实际请求服务 + API 版本」的废弃关系。
// 服务名与 API 版本共同识别废弃对象，避免误标同服务新版或其他服务的同名版本。
type deprecatedService struct {
	// productName 是迁移提示中展示的产品名称。
	productName string
	// requestService 是签名发往的实际请求服务名（serviceMapping 归一化后）。
	requestService string
	// deprecatedVersion 是被废弃的 API 版本。
	deprecatedVersion string
	// replacementVersion 是建议迁移的 API 版本。
	replacementVersion string
	// canonicalService 是旧版规范命令，即 RootSupport 中的包名，如 rdsmysql。
	canonicalService string
	// hiddenSpellings 是默认发现中需要隐藏的全部命令写法（规范名与兼容别名）。
	hiddenSpellings []string
	// newServiceCommand 是新版规范命令，如 rdsmysqlv2。
	newServiceCommand string
}

// deprecatedServices 是本期本地废弃记录，按设计仅包含 RDS MySQL 2018。
var deprecatedServices = []deprecatedService{{
	productName:        "RDS MySQL",
	requestService:     "rds_mysql",
	deprecatedVersion:  "2018-01-01",
	replacementVersion: "2022-01-01",
	canonicalService:   "rdsmysql",
	hiddenSpellings:    []string{"rdsmysql", "rds_mysql"},
	newServiceCommand:  "rdsmysqlv2",
}}

// isDefaultHiddenService 报告某个默认发现入口（规范服务名或兼容别名）是否应隐藏。
// 显式输入旧服务不经过可见性判断，调用能力保持不变。
func isDefaultHiddenService(service string) bool {
	for _, d := range deprecatedServices {
		for _, spelling := range d.hiddenSpellings {
			if spelling == service {
				return true
			}
		}
	}
	return false
}

// deprecatedServiceByCanonical 按 RootSupport 包名（如 rdsmysql）查找废弃记录，
// 用于服务帮助和 MCP list_apis 等只有规范服务名的场景。
func deprecatedServiceByCanonical(service string) (deprecatedService, bool) {
	for _, d := range deprecatedServices {
		if d.canonicalService == service {
			return d, true
		}
	}
	return deprecatedService{}, false
}

// deprecatedServiceForCall 按标准化后的实际请求服务名与最终 API 版本查找废弃记录，
// 用于调用提示。命令字符串本身不作为判断依据。
func deprecatedServiceForCall(requestService, version string) (deprecatedService, bool) {
	for _, d := range deprecatedServices {
		if d.requestService == requestService && d.deprecatedVersion == version {
			return d, true
		}
	}
	return deprecatedService{}, false
}

// warnDeprecatedServiceCall 在最终请求服务与版本命中废弃记录时，向 stderr 输出一次
// 迁移提示。提示不改写请求、不改变退出码，stdout 业务输出保持原契约。
// VE_NO_DEPRECATION_WARNING=1 关闭调用告警，帮助和 MCP API 描述中的提示不受影响。
// serviceName 为 RootSupport 包名或用户输入服务名，先经 serviceMapping 标准化。
func warnDeprecatedServiceCall(serviceName, version string) {
	if os.Getenv("VE_NO_DEPRECATION_WARNING") == "1" {
		return
	}
	requestService := serviceName
	if mapped, ok := GetServiceMapping(serviceName); ok {
		requestService = mapped
	}
	d, ok := deprecatedServiceForCall(requestService, version)
	if !ok {
		return
	}
	fmt.Fprintln(os.Stderr, d.callWarning())
}

// callWarning 构造终端与 MCP 共用的 warning 文案（固定三行）。
func (d deprecatedService) callWarning() string {
	return strings.Join([]string{
		trf("Warning: %s API %s is deprecated.", d.productName, d.deprecatedVersion),
		trf("Please migrate to API %s using ve %s.", d.replacementVersion, d.newServiceCommand),
		trf("The current request will continue using API %s.", d.deprecatedVersion),
	}, "\n")
}

// helpNotice 构造旧服务帮助顶部的废弃与迁移提示（纯文本，直接拼到 usage 模板前）。
func (d deprecatedService) helpNotice() string {
	return strings.Join([]string{
		trf("Deprecated: %s API %s is deprecated.", d.productName, d.deprecatedVersion),
		trf("Please migrate to API %s using ve %s.", d.replacementVersion, d.newServiceCommand),
		trf("The current command and its actions continue to use API %s.", d.deprecatedVersion),
	}, "\n") + "\n\n"
}

// mcpAPIDescriptionPrefix 构造 MCP list_apis 旧 API description 字段中的迁移提示，
// 保持单行与既有字段名称、类型不变。
func (d deprecatedService) mcpAPIDescriptionPrefix() string {
	return trf("Deprecated: %s API %s is deprecated. Migrate to API %s using service code %s.",
		d.productName, d.deprecatedVersion, d.replacementVersion, d.newServiceCommand)
}
