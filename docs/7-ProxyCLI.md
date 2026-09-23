[← MCP Server](6-MCP.md) | Proxy-CLI [(中文)](7-ProxyCLI-zh.md)

---

## Proxy-CLI

Proxy-CLI integrates Volcengine product command-line tools with VE, giving users and agents a unified entry point for product capabilities. General cloud resource APIs and product-specific workflows can work through the same CLI, reducing the need to manage separate tool installations and execution environments across products.

Each product CLI continues to provide its own domain capabilities. VE handles integration, on-demand installation, and execution handoff while preserving the product's command semantics, authentication settings, input, and output.

## Quick start

**Prefix the product's native command with `ve`, keeping all remaining subcommands and arguments unchanged.** For example, `arkcli --help` becomes `ve arkcli --help`, and `tosutil ls tos://example-bucket` becomes `ve tosutil ls tos://example-bucket`.

```shell
ve <native-product-command> <subcommand or arguments...>
```

### 1. Discover available products

```shell
ve proxycli list
ve proxycli info tls-cli
```

`list` shows products available in your VE build; only entries marked `ready` can be invoked. `info` provides the product description, supported platforms, and official documentation. If your VE build lacks these entry points, use a version that supports the corresponding product proxies.

### 2. Invoke the product CLI directly

You do not need to install the product tool manually first:

```shell
ve arkcli --help
ve tosutil help
ve volclog --help
ve milvus-cli version
```

The first invocation downloads and installs the tool if needed. Later calls reuse it. Even a help or version request may trigger the first installation.

For product operations, configure authentication according to that product's guide and replace example resource names before running:

```shell
# Native command: tosutil ls tos://example-bucket
ve tosutil ls tos://example-bucket
```

### 3. Pass arguments to the product CLI

Place product arguments after the native command:

```shell
ve milvus-cli --profile prod --help
```

Here, Milvus CLI handles `--profile`. Each product keeps its own authentication, profiles, regions, and output formats; VE configuration is not automatically converted into every product's configuration. Product arguments must follow the product command.

### 4. Install and upgrade tools

Separate installation is usually unnecessary. To preinstall a tool or upgrade its managed version:

```shell
ve proxycli install tls-cli
ve proxycli upgrade tls-cli --yes
```

Management commands use product IDs, while direct invocation uses native command names. For TLS, the ID is `tls-cli` and the invocation is `ve volclog`. Explicit execution by ID is also available:

```shell
ve proxycli run tls-cli -- --version
```

Managed executables are installed under `~/.volcengine/proxy-cli/bin/` by default. VE prefers an existing usable native program on PATH; its original package manager continues to manage that installation. `ve upgrade` upgrades the VE host, while `ve proxycli upgrade <product-id>` upgrades a managed product CLI.

Agents can read product IDs, native commands, and readiness with `ve proxycli list --output json`, then follow the product's documentation. Standard input, output, and exit codes retain the product's original semantics.

## On-demand loading

Product CLIs are distributed separately from the VE host. Installing VE does not install all product tools by default. A product tool is checked only when its capability is invoked, and downloaded and installed if no usable local program is available.

| Stage | Product CLI behavior |
| --- | --- |
| Install VE or use existing OpenAPI and configuration features | No product CLI downloads, installations, or processes are triggered |
| Use a product capability for the first time | Check for a local program and install only the required tool if it is missing |
| Use the same product again | Reuse the available installed program |
| Leave other products unused | Do not load or install their tools |

Users who only need native VE OpenAPI and configuration features do not have to preinstall product CLIs or configure additional environments for them. Those workflows remain unchanged; product tools are installed and started only when their capabilities are used.

## Product capabilities and official documentation

Each product CLI covers its own specialized scenarios. Consult its official documentation and your VE version for available capabilities, supported platforms, and authentication requirements. Some product documentation is available in Chinese.

| Product capability | Product CLI | VE invocation | Official documentation |
| --- | --- | --- | --- |
| Object Storage TOS | tosutil | `ve tosutil help` | [Official documentation](https://www.volcengine.com/docs/6349/148775) |
| Volcengine Ark | arkcli | `ve arkcli --help` | [Official documentation](https://docs.volcengine.com/docs/82379/2536875) |
| Log Service TLS | volclog | `ve volclog --help` | [Official documentation](https://docs.volcengine.com/docs/6470/2559490) |
| Function Service veFaaS | vefaas | `ve vefaas --help` | [Official documentation](https://docs.volcengine.com/docs/6662/2222397) |
| Object Storage TOS | ve-tos-cli | `ve ve-tos-cli --help` | [Official documentation](https://docs.volcengine.com/docs/6349/2563185) |
| ADrive | ve-adrive-cli | `ve ve-adrive-cli --help` | [Official documentation](https://docs.volcengine.com/docs/88503/2644283) |
| Streaming Flink | volc-flink | `ve volc-flink --help` | [Official documentation](https://docs.volcengine.com/docs/6581/2488139) |
| AI-native BaaS: Supabase | byted-supabase-cli | `ve byted-supabase-cli --help` | [Official documentation](https://docs.volcengine.com/docs/87275/2549800) |
| Vector Database Milvus | milvus-cli | `ve milvus-cli version` | [Official documentation](https://docs.volcengine.com/docs/85665/2516795) |
| Viking vector, knowledge and memory services | viking-cli | `ve viking-cli version` | [Official documentation](https://docs.volcengine.com/docs/84313/2359553) |
| AI MediaKit | mediakit-cli | `ve mediakit-cli --help` | [Official documentation](https://docs.volcengine.com/docs/6448/2523671) |
| Real-Time Communication RTC | vertc | `ve vertc --help` | [Official documentation](https://docs.volcengine.com/docs/6348/2644277) |
| Edge Functions and Edge KV | nest | `ve nest --help` | [Official documentation](https://docs.volcengine.com/docs/6454/2179402) |
| Observability and troubleshooting: VeOps | veops-cli | `ve veops-cli version` | [Official documentation](https://docs.volcengine.com/docs/86845/2563136) |
| Localization: Starling | starling | `ve starling --help` | [Official documentation](https://docs.volcengine.com/docs/4640/2122756) |
| OpenViking | ov | `ve ov --version` | [Official documentation](https://docs.volcengine.com/docs/84313/2533512) |

TOS uses `help` for help; Milvus, Viking, and VeOps use `version` for version information. On first use of OpenViking, run `ve ov language en` before requesting help. Starling currently provides amd64 binaries; platform support depends on the product and your VE build.

---

[← MCP Server](6-MCP.md) | [Back to project overview](../README.EN.MD)
