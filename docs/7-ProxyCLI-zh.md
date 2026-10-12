[← MCP 服务](6-MCP-zh.md) | Proxy-CLI [(English)](7-ProxyCLI.md)

---

## Proxy-CLI

Proxy-CLI 将火山引擎各产品的命令行工具接入 VE，为用户和 Agent 提供统一的产品能力入口。通用云资源接口与产品专属工作流可以围绕同一个 CLI 协作，减少跨产品时分别管理工具安装和执行环境的负担。

各产品 CLI 继续提供自己的领域能力。VE 负责产品工具的接入、按需安装和执行衔接，保留产品原有的命令语义、认证配置和输入输出方式。

## 快速使用

**在产品原生命令前加上 `ve`，后面的子命令和参数保持不变。** 例如，原生 `arkcli --help` 对应 `ve arkcli --help`，原生 `tosutil ls tos://example-bucket` 对应 `ve tosutil ls tos://example-bucket`。

```shell
ve <产品原生命令> <子命令或参数...>
```

### 1. 查看当前支持的产品

```shell
ve proxycli list
ve proxycli info tls-cli
```

`list` 展示当前版本包含的产品；只有 `ready` 状态可以调用。`info` 提供产品说明、支持平台和官方文档。若当前 VE 没有这些入口，需要使用支持相应产品代理的版本。

### 2. 直接调用产品 CLI

无需先手动安装产品工具，直接运行即可：

```shell
ve arkcli --help
ve tosutil help
ve volclog --help
ve milvus-cli version
```

首次调用会在需要时下载并安装对应工具，之后复用本地程序。帮助或版本查询也可能触发首次安装。

执行实际产品操作时，先按该产品的官方文档配置认证并替换示例中的资源名称：

```shell
# 原生命令：tosutil ls tos://example-bucket
ve tosutil ls tos://example-bucket
```

### 3. 将参数交给产品 CLI

产品参数放在原生命令之后：

```shell
ve milvus-cli --profile prod --help
```

此处的 `--profile` 由 Milvus CLI 处理。各产品的认证、profile、region 和输出格式遵循各自文档；VE 的配置不会自动转换为所有产品的配置。不要将产品参数放在产品命令之前。

### 4. 安装与升级

通常不需要单独安装；需要提前准备工具或升级托管版本时，可以执行：

```shell
ve proxycli install tls-cli
ve proxycli upgrade tls-cli --yes
```

管理命令使用产品ID，实际调用使用原生命令名。例如，TLS的ID是 `tls-cli`，调用入口是 `ve volclog`。也可以按ID显式执行：

```shell
ve proxycli run tls-cli -- --version
```

VE默认将托管程序安装到 `~/.volcengine/proxy-cli/bin/`。如果PATH中已有可用的原生程序，VE优先使用它，外部安装仍由原包管理器维护。`ve upgrade` 升级的是VE宿主，`ve proxycli upgrade <产品ID>` 升级的是托管的产品CLI。

Agent可使用 `ve proxycli list --output json` 读取产品ID、原生命令和状态，再按产品文档调用；产品的标准输入、输出和退出码保持原有语义。

## 按需加载（冷加载）

产品 CLI 与 VE 宿主独立分发。默认安装 VE 时不会同时安装所有产品工具；使用某个产品能力时，才检查该工具是否已有可用的本地程序，必要时下载并安装。

| 使用阶段 | 产品 CLI 的行为 |
| --- | --- |
| 安装 VE、使用原有 OpenAPI 或配置功能 | 不触发产品 CLI 的下载、安装或启动 |
| 首次使用某个产品能力 | 检查本地程序，缺失时仅安装该产品所需的工具 |
| 再次使用同一产品 | 复用已安装的可用程序 |
| 未使用其他产品 | 不加载或安装其他产品工具 |

因此，只使用 VE 原生 OpenAPI 与配置等功能的用户无需预装产品 CLI，也无需为它们额外配置运行环境；这些功能的使用方式保持不变。产品工具的安装与启动只发生在对应能力被使用时。

## 产品能力与官方文档

产品 CLI 覆盖各自的专业场景。具体能力、支持平台和认证要求以各产品官方文档及所使用的 VE 版本为准。

| 产品能力 | 产品 CLI | VE 调用示例 | 官方文档 |
| --- | --- | --- | --- |
| 对象存储 TOS | tosutil | `ve tosutil help` | [官方文档](https://www.volcengine.com/docs/6349/148775) |
| 火山方舟 Ark | arkcli | `ve arkcli --help` | [官方文档](https://docs.volcengine.com/docs/82379/2536875) |
| 日志服务 TLS | volclog | `ve volclog --help` | [官方文档](https://docs.volcengine.com/docs/6470/2559490) |
| 函数服务 veFaaS | vefaas | `ve vefaas --help` | [官方文档](https://docs.volcengine.com/docs/6662/2222397) |
| 对象存储 TOS | ve-tos-cli | `ve ve-tos-cli --help` | [官方文档](https://docs.volcengine.com/docs/6349/2563185) |
| 智能网盘 ADrive | ve-adrive-cli | `ve ve-adrive-cli --help` | [官方文档](https://docs.volcengine.com/docs/88503/2644283) |
| 流式计算 Flink | volc-flink | `ve volc-flink --help` | [官方文档](https://docs.volcengine.com/docs/6581/2488139) |
| AI 原生 BaaS：Supabase | byted-supabase-cli | `ve byted-supabase-cli --help` | [官方文档](https://docs.volcengine.com/docs/87275/2549800) |
| 向量数据库 Milvus | milvus-cli | `ve milvus-cli version` | [官方文档](https://docs.volcengine.com/docs/85665/2516795) |
| Viking 向量库、知识库与记忆库 | viking-cli | `ve viking-cli version` | [官方文档](https://docs.volcengine.com/docs/84313/2359553) |
| AI MediaKit | mediakit-cli | `ve mediakit-cli --help` | [官方文档](https://docs.volcengine.com/docs/6448/2523671) |
| 实时音视频 RTC | vertc | `ve vertc --help` | [官方文档](https://docs.volcengine.com/docs/6348/2644277) |
| 边缘函数与边缘 KV | nest | `ve nest --help` | [官方文档](https://docs.volcengine.com/docs/6454/2179402) |
| 可观测与排障 VeOps | veops-cli | `ve veops-cli version` | [官方文档](https://docs.volcengine.com/docs/86845/2563136) |
| 国际化翻译 Starling | starling | `ve starling --help` | [官方文档](https://docs.volcengine.com/docs/4640/2122756) |
| OpenViking | ov | `ve ov --version` | [官方文档](https://docs.volcengine.com/docs/84313/2533512) |

TOS查看帮助使用 `help`；Milvus、Viking和VeOps查看版本使用 `version`。OpenViking首次使用先执行 `ve ov language zh-CN`，再查看帮助。Starling当前提供amd64程序，平台支持以产品文档及当前VE构建为准。

---

[← MCP 服务](6-MCP-zh.md) | [返回项目概览](../README.MD)
