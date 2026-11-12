// deprecated_services.go 在非生成源码中维护服务版本废弃/下线记录。
//
// CLI 对记录命中的「实际请求服务 + 最终 API 版本」直接阻断：不发起请求、
// 返回不兼容版本错误。记录同时用于默认入口隐藏与帮助提示。
//
// 不修改内嵌 metadata/structure/metatype/bindata，不提供绕过开关。
package cmd

import (
	"fmt"
	"strings"
)

// deprecatedService 描述一条「实际请求服务 + API 版本」的下线关系。
// 服务名与 API 版本共同识别对象，避免误伤同服务新版或其他服务的同名版本。
type deprecatedService struct {
	// productName 是提示中展示的产品名称。
	productName string
	// requestService 是签名发往的实际请求服务名（serviceMapping 归一化后）。
	requestService string
	// deprecatedVersion 是已下线、禁止调用的 API 版本。
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

// deprecatedServices 是本期本地记录，目前仅包含 RDS MySQL 2018（CLI 已直接下线）。
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

// deprecatedServiceByCanonical 按 RootSupport 包名（如 rdsmysql）查找记录，
// 用于服务/action 帮助和 MCP 显式查询拦截。
func deprecatedServiceByCanonical(service string) (deprecatedService, bool) {
	for _, d := range deprecatedServices {
		if d.canonicalService == service {
			return d, true
		}
	}
	return deprecatedService{}, false
}

// deprecatedServiceForCall 按标准化后的实际请求服务名与最终 API 版本查找记录。
// 命令字符串本身不作为判断依据。
func deprecatedServiceForCall(requestService, version string) (deprecatedService, bool) {
	for _, d := range deprecatedServices {
		if d.requestService == requestService && d.deprecatedVersion == version {
			return d, true
		}
	}
	return deprecatedService{}, false
}

// rejectDeprecatedServiceCall 在最终请求服务与版本命中下线记录时返回不兼容版本错误。
// 调用方必须在发起任何网络请求前返回该错误；不提供绕过开关。
// serviceName 为 RootSupport 包名或用户输入服务名，先经 serviceMapping 标准化。
func rejectDeprecatedServiceCall(serviceName, version string) error {
	requestService := serviceName
	if mapped, ok := GetServiceMapping(serviceName); ok {
		requestService = mapped
	}
	d, ok := deprecatedServiceForCall(requestService, version)
	if !ok {
		return nil
	}
	return d.retiredCallError()
}

// retiredCallError 构造终端与 MCP 共用的不兼容版本错误（两行，写 stderr / 工具错误）。
func (d deprecatedService) retiredCallError() error {
	return fmt.Errorf("%s", strings.Join([]string{
		trf("%s API %s has been retired and is no longer available.", d.productName, d.deprecatedVersion),
		trf("Please migrate to API %s using ve %s.", d.replacementVersion, d.newServiceCommand),
	}, "\n"))
}

// helpNotice 构造旧服务帮助顶部的下线提示（帮助仍可查看，调用一律阻断）。
func (d deprecatedService) helpNotice() string {
	return strings.Join([]string{
		trf("%s API %s has been retired and can no longer be called from the CLI.", d.productName, d.deprecatedVersion),
		trf("Please migrate to API %s using ve %s.", d.replacementVersion, d.newServiceCommand),
	}, "\n") + "\n\n"
}
