[← Configuration](3-Configuration.md) | Usage[(中文)](4-Usage-zh.md) | [Advanced Usage →](5-Advanced.md)

---

## Usage

Basic command format:

```shell
ve <service> <action> [--Param value ...] [--header Name=Value ...] [--body json]
                      [--api-param Name=Value ...]
                      [--profile name] [--region region] [--endpoint endpoint] [--lang language]
                      [--version api-version] [--method GET|POST] [--force]
                      [--output json|table|table-num|text|yaml|off] [--query jmespath]
```

Argument kinds:

- **API parameters**: double-dash `--Param value` (enter request body/query; reserved names `body` / `header` / `api-param` excluded)
- **Public system flags** (after the action): `--profile` / `--region` / `--endpoint` / `--lang` / `--version` / `--method` / `--force` / `--output` / `--query`
- **Reserved double-dash controls**: `--header` (HTTP headers), `--body` (JSON body), and force-only `--api-param Name=Value` (explicit API parameter); the controls themselves are **not** API parameters

System flags in API calls use double hyphens and are placed after the action. If an action exposes an exact-name API parameter (case-sensitive), the double-dash form is parsed as the API parameter.

### Flag Prefix Contract

The CLI has exactly one public flag prefix: **double dash `--name`**. System flags, API parameters and the reserved controls (`--header` / `--body` / `--api-param`) all use double dashes.

- Help output, shell completion, error messages and documentation examples show the `--name` form only.
- Conflicts are case-sensitive and resolved against the parameters the current action actually exposes: `--Region` and `--Lang` are always API parameters.
- When an action exposes an API parameter with the **exact** name of a system flag, the double-dash form after that action is parsed as the API parameter; use an equivalent non-flag route for the system behaviour (see Known Name Conflicts).

## Discover Services and Actions

List supported services:

```shell
ve --help
```

List actions under a service:

```shell
ve ecs --help
```

Show action parameters:

```shell
ve ecs DescribeInstances --help
```

By default, `-h` / `--help` uses concise mode and shows parameter names, types, and required status without loading the full parameter corpus. Use detail mode to include descriptions and examples:

```shell
ve ecs DescribeInstances -h --detail
ve ecs DescribeInstances --help --detail
```

Using `--detail` by itself does not trigger help.

Show version:

```shell
ve version
ve -v
```

## Call APIs

Call without parameters:

```shell
ve sts GetCallerIdentity
```

Call with parameters:

```shell
ve ecs DescribeInstances --InstanceIds.1 i-1234567890abcdef0
```

Multiple parameters:

```shell
ve rds_mysql ListDBInstanceIPLists --InstanceId mysql-xxxxxx --GroupName default
```

Parameter names and values are separated by spaces. The supported syntax is:

```shell
--Param value
--region cn-beijing
```

Do not use `--Param=value`, `--region=cn-beijing`, or `--lang=ZH`. Flag names and values must be separated by a space.

## CLI System Flags

Public system flags use the standard double-hyphen form:

| Flag | Purpose |
| --- | --- |
| `--profile` | Use a specific profile for this invocation without changing current |
| `--region` | Override region for this invocation |
| `--endpoint` | Override endpoint for this invocation and clear endpoint resolver |
| `--lang` | Set the language of CLI-owned help, prompts, and errors for this invocation |
| `--version` | Set the **API version** for this call; if omitted, uses the bundled service version (not the CLI binary version from root `ve -v` / `ve --version` / `ve version`) |
| `--force` | Skip service/action metadata validation and force-call unlisted or newly released APIs; **unlisted services** require `--version` and a fixed endpoint (`--endpoint` or profile/`VOLCENGINE_ENDPOINT` when resolver is not `standard`); bundled services can fall back to metadata. Presence-only: write `--force` alone, not `--force true` |
| `--method` | HTTP method (`GET`/`POST`); same rules on normal and `--force` paths: explicit value wins, else action metadata, else `GET` |
| `--output` | API response format: `json` (default), `table`, `table-num`, `text`, `yaml`, `off` |
| `--query` | JMESPath expression to filter/project the full response JSON (including `ResponseMetadata` and `Result`) before formatting |

After the action, a double-dash flag whose exact case-sensitive name is exposed by that action is parsed as an API parameter. Without such a conflict, it is parsed as a system flag.

Names with different casing, such as `--Region` or `--Endpoint`, are always API parameters.


### Reserved Double-Dash Controls

| Flag | Purpose |
| --- | --- |
| `--header Name=Value` | Add an HTTP request header; **repeatable**; never enters the request body. `Content-Type` overrides metadata; last value wins for the same name |
| `--body json` | JSON request body for `application/json` style calls; mutually exclusive with other API parameters |
| `--api-param Name=Value` | Add an explicit API business parameter; **repeatable and available only with `--force`**. Primarily resolves system-name conflicts such as an unlisted API's business parameters named `query` or `output` |

```shell
ve sts GetCallerIdentity --header X-Custom-Trace=abc
ve newsvc Act --force --version 2024-01-01 --endpoint open.volcengineapi.com \
  --header Content-Type=application/json \
  --header X-Feature=on \
  --body '{"k":1}'
ve newsvc Search --force --version 2024-01-01 --endpoint open.volcengineapi.com \
  --query 'Result.Items' --output table \
  --api-param 'query=server-side-filter' --api-param 'output=compact'
```

Notes:

- Override `Content-Type` with `--header Content-Type=...`; forms with parameters (e.g. `application/json; charset=utf-8`) are still treated as JSON
- With `--body` and no metadata, Content-Type defaults to `application/json`
- `--header` can be used with `--body`; headers are not flattened API params and do not conflict with `--body`
- Blocked header names: `Host`, `Authorization`, `Content-Length` (transport/signing)
- `--api-param` splits on the first `=` only, trims the parameter name while preserving its case, and permits an empty value. Duplicate names or a same-case collision with a direct `--Name value` are errors rather than last-value-wins
- `--api-param` is expanded before request construction, works for both JSON and query/form calls, and never enters the request itself
- Reserved names: `--header`, `--body`, and `--api-param` cannot be used as ordinary API parameter names

Examples:

```shell
# Use a specific profile
ve ecs DescribeInstances --profile prod

# Use a specific profile and override region
ve ecs DescribeInstances --profile prod --region ap-southeast-1

# Override only region
ve ecs DescribeInstances --region cn-shanghai

# Specify endpoint for an STS call
ve sts GetCallerIdentity --region cn-beijing --endpoint sts.volcengineapi.com
```

If `--profile` references a profile that does not exist, the command returns an error.

### Known Name Conflicts

Known exact-name conflicts include:

- `--lang` on `i18nopenapi VideoProjectSuppressionStart`: after that action `--lang` is the API parameter; switch the CLI display language through the `LC_ALL` / `LC_MESSAGES` / `LANG` environment variables instead (see Display Language)
- `--query` on `insight AgentChat`: after that action `--query` is the API parameter; keep the default JSON output and filter it downstream

The same rule applies if other actions later expose colliding names: the double-dash form after that action is the API parameter.

For an unlisted action, metadata cannot declare a collision, so `--query` and `--output` retain their public system meanings. With `--force`, pass same-named business parameters explicitly as `--api-param query=<value>` and `--api-param output=<value>`; both system and business values can then be used in one call without ambiguity. This explicit route also works with a bundled action when `--force` is set, but it cannot override a directly supplied same-case API parameter.

### Display Language

Use `--lang EN` for English or `--lang ZH` for Simplified Chinese. Locale forms such as `en-US`, `en_US`, `zh-CN`, `zh_CN`, and `zh-Hans` are also accepted. Unsupported values fall back to English.

When `--lang` is omitted, the CLI checks `LC_ALL`, `LC_MESSAGES`, and `LANG` in that order, then falls back to English. The explicit flag takes precedence and is not persisted to the configuration file.

```shell
ve sts GetCallerIdentity --lang ZH --help
ve ecs DescribeInstances --lang EN --help
ve login --lang zh-CN
```

Language selection only affects text generated by the CLI. API response bodies and server-provided content are not translated or modified.

## JSON Parameters

For query/form APIs, if a parameter value is a JSON object or JSON array, the CLI attempts to parse it as JSON:

```shell
ve rds_mysql ModifyDBInstanceIPList \
  --InstanceId mysql-xxxxxx \
  --GroupName default \
  --IPList '["10.20.30.40","50.60.70.80"]'
```

String parameters are kept as strings and are not forcibly parsed just because they look like JSON.

## application/json Requests

For APIs whose `ContentType` is `application/json`, pass a JSON body directly:

```shell
ve rds_mysql ModifyDBInstanceIPList \
  --body '{"InstanceId":"mysql-xxxxxx","GroupName":"default","IPList":["10.20.30.40","50.60.70.80"]}'
```

`--body` must be a JSON object or JSON array. It cannot be mixed with flattened parameters:

```shell
# Wrong: --body cannot be used together with other API parameters
ve rds_mysql ModifyDBInstanceIPList --body '{"InstanceId":"mysql-xxxxxx"}' --GroupName default
```

application/json APIs also support dotted keys. The CLI expands them into nested JSON using metadata:

```shell
ve some_service SomeJsonAction \
  --Name demo \
  --Ports.1 80 \
  --Ports.2 443 \
  --Tags.1.Key env \
  --Tags.1.Value prod
```

Array indices are 1-based and must be contiguous. `0`, negative indices, and skipped indices are errors.

## Arrays and Nested Parameters

Common array syntax:

```shell
ve ecs DescribeInstances --InstanceIds.1 i-123 --InstanceIds.2 i-456
```

Array of objects:

```shell
ve some_service SomeAction \
  --Filters.1.Key InstanceType \
  --Filters.1.Values.1 ecs.g1.large \
  --Filters.1.Values.2 ecs.g2.large
```

For application/json APIs, dotted keys are restored to nested objects and arrays. For non-JSON APIs, dotted keys are preserved and handled by the service/API layer.

## Unknown Parameters

The CLI allows unknown API parameters to pass through to the service/API layer. Unless the parameter path itself is invalid, the CLI does not reject a parameter only because it is absent from metadata.

Example:

```shell
ve ecs DescribeInstances --NewServerSideParam value
```

This is useful when the service has added a parameter but local metadata has not been updated yet.

## Unlisted Services and Actions

The CLI validates services and actions against built-in metadata. If the **service or action is not yet bundled**, use `--force` to bypass validation; unlisted services also require `--version` and a **fixed** endpoint (`--endpoint`, or profile / `VOLCENGINE_ENDPOINT` when `endpoint-resolver` is not `standard`) because the CLI has no metadata from which to resolve a host. Bundled services can omit these overrides in force mode and use metadata with the same endpoint rules as normal calls. See [Advanced Usage: Force Invocation](5-Advanced.md#force-invocation).

```shell
ve newservice DescribeNewResource \
  --version 2024-01-01 \
  --endpoint open.volcengineapi.com \
  --SomeParam value \
  --force
```

For an unlisted query/form API whose business parameters are named `query` and `output`, keep the public flags as response controls and use the explicit force-only business-parameter route:

```shell
ve newservice Search \
  --version 2024-01-01 \
  --endpoint open.volcengineapi.com \
  --force \
  --query 'Result.Items' \
  --output table \
  --api-param 'query=status=active' \
  --api-param 'output='
```

## Common Scenarios

Use current profile:

```shell
ve ecs DescribeInstances
```

Use a non-current profile:

```shell
ve ecs DescribeInstances --profile prod
```

Use environment-based default credential chain:

```shell
export VOLCENGINE_ACCESS_KEY=AK
export VOLCENGINE_SECRET_KEY=SK
export VOLCENGINE_REGION=cn-beijing
ve ecs DescribeInstances
```

Use an OIDC profile:

```shell
ve configure set --profile ci-oidc --mode oidc --region cn-beijing \
  --oidc-token-file /var/run/secrets/oidc-token \
  --role-trn trn:iam::2100000000:role/CIRole

ve ecs DescribeInstances --profile ci-oidc
```

Use an ECS instance role profile:

```shell
ve configure set --profile ecs-role --mode ecsrole --region cn-beijing --role-name MyRole
ve ecs DescribeInstances --profile ecs-role
```

## Common Errors

Missing credentials:

```text
credentials not configured, please run 've login' or 've configure set', or set VOLCENGINE_ACCESS_KEY and VOLCENGINE_SECRET_KEY environment variables
```

Missing region:

```text
region not set, please set it via profile, --region flag, or VOLCENGINE_REGION environment variable
```

Public system flags (double-dash): `--profile`, `--region`, `--endpoint`, `--lang`, `--force`, `--version`, `--method`, `--output`, `--query`.
Reserved double-dash controls: `--header`, `--body`, `--api-param` (see “Reserved Double-Dash Controls” above).

## Filtering and Output Formats

After a successful API call, the CLI prints the **full response JSON** (typically `ResponseMetadata` + `Result`) to stdout by default. Use system flags to control presentation:

| Flag | Description |
|------|-------------|
| `--output` | Format: `json` (default), `table`, `table-num`, `text`, `yaml`, `off` |
| `--query` | JMESPath applied **before** formatting; paths are relative to the full response (list data is usually under `Result.*`) |

Pipeline: `raw response → [--query] → [--output] → stdout` (query before format; Volcengine envelope field paths).

```shell
# Project then table (recommended for list APIs; use --query to pick the fields)
# Column order follows the multi-select hash you wrote: the hash below is
# Name, Id, Status, so the columns are Name, Id, Status.
ve ecs DescribeInstances \
  --query 'Result.Instances[*].{Name:InstanceName,Id:InstanceId,Status:Status}' \
  --output table

# Row numbers: table-num adds a leading # column starting at 1
ve ecs DescribeInstances \
  --query 'Result.Instances[*].{Name:InstanceName,Id:InstanceId}' \
  --output table-num

# Tab-separated text for awk/grep (one record per line, so `nl` numbers records)
ve sts GetCallerIdentity --query 'Result.AccountId' --output text
ve ecs DescribeInstances --query 'Result.Instances[*].{Id:InstanceId}' --output text | nl

# Without --query, table strips ResponseMetadata and splits nested data into titled sections.
ve sts GetCallerIdentity --output table

# YAML
ve sts GetCallerIdentity --output yaml

# Exit code only (API call still runs)
ve ecs DescribeInstances --output off
```

Notes:

- **`ResponseMetadata` stripping (display layer)**: **without `--query`**, `table` / `table-num` / `text` drop the top-level `ResponseMetadata` before rendering so the output shows the payload directly. **With an explicit `--query`, nothing is stripped** — the query result is exactly what you selected, so `--query 'ResponseMetadata.RequestId'` returns its value and `--query '@'` shows the full response verbatim. `json` / `yaml` **always keep the full response** (including `RequestId`), so scripted consumers are unaffected. A response containing only `ResponseMetadata` (common for write APIs) is left intact in both cases so it does not look empty. A nested field that happens to be named `ResponseMetadata` is payload and is never removed.
- **Nested sections**: `table` splits nested objects and record lists into separate titled sections (the title is the field path, e.g. `Result.Instances.Tags[1]`) instead of dumping JSON into a cell. A nested field **also stays as a main-table column**, where the cell reads `(see section)` and points at the matching section; when the same field is a scalar, `null` or an empty list on some records, those values are still shown in the main table rather than being dropped because another record nests it. Section numbering starts at 1 and matches the `#` column of `table-num`, so a section can be traced back to its record. No number is added when the parent list holds a single record. Lists of plain scalars (e.g. `["sg-1","sg-2"]`) stay inline in the cell.
- **Single-record verticalization**: a single object (and any single-row result) renders as one horizontal record — a field-name header row plus a value row, matching the AWS CLI. Only when the terminal width **is known** and that row is wider than it, the record is transposed into a two-column `Field | Value` table to avoid horizontal scrolling. Multi-row results and any case where the width is unknown (redirected output, pipes, failed probe) keep the horizontal layout.
- **Terminal width fitting**: when writing to a terminal the width is detected automatically; over-wide grids shrink the widest column first and wrap cell content onto additional physical rows without discarding response values. Every column keeps a minimum readable width. Redirected or piped output is not wrapped, so each value stays on one complete line.
- **Column order**: with a `--query` multi-select hash (`{Key:Path,...}`), `table` / `table-num` / `text` follow the order you wrote — **for both record lists and a single object** (for a single object this is the header-column order). Everything else (no `--query`, plain path projection, expressions such as `merge()` where the order cannot be determined statically, or duplicate keys in the hash) falls back to alphabetical field order. A hint that does not match the actual fields is discarded as a whole, so ordering is never applied partially and no column is lost. Write an explicit multi-select hash when you need a fixed column order.
- **Row numbers**: `table-num` adds a leading `#` column to record results. A single object is one record, so it is numbered `1`; a list is numbered from 1 in order. Numbering starts at 1 and exists for human reference; scripts should read values via `--output json` / `text` rather than the `#` column.
- **Color**: when `enableColor` is on and output goes to a terminal, `table` / `table-num` style headers and cells; redirected output, pipes and `NO_COLOR` disable it. Styling never affects column widths or alignment.
- Do not pipe `--output table` through `nl`: `nl` numbers borders and the header too, so the numbers no longer line up with data rows. Use `--output table-num` for tables, or `--output text | nl` for TSV.
- `table` / `table-num` / `text` render newlines, tabs, and terminal control characters as visible escapes so response data cannot break row/column boundaries or inject terminal controls.
- For a stable human-readable output contract, booleans are rendered as `True` / `False` in `table`, `table-num`, and `text`; `json` and `yaml` keep their native lowercase `true` / `false` syntax.
- On name conflicts (e.g. `insight AgentChat` `--query`), the double-dash form after that action is the API parameter, so the same-named system flag is unavailable for that call; see Known Name Conflicts.
- Empty lists: `table` / `table-num` print `(empty)`; `text` prints no lines (easy empty check in scripts). A missing/null `--query` path prints `None` in table/text. An empty object `{}` is not an empty list: table prints a header-only record with no value row; text prints no lines. A non-empty list made only of empty objects or empty positional records keeps one `{}` or `[]` row per record in table output (and remains numbered by `table-num`), while text has no fields or values to print.
- `text` output is not type-distinguishing: an empty list `[]` and an empty object `{}` both print no lines; a missing/null `--query` path prints `None`; the literal string `None` is also rendered as `None`. Use `--output json` when you need unambiguous type or emptiness checks.
- **`text` recursively flattens any response**: like the AWS CLI text formatter, `text` never prints a JSON blob. A bare `text` (no `--query`) is flattened to TSV: an object's scalar fields become one row, and each nested object/list recurses onto its own line(s) prefixed by the UPPERCASED source key (for example `INSTANCELIST\t...`). A list of objects shares one column set (missing field → `None`) with one row per object. Deeper positional or object-list projections are expanded recursively, nested empty lists or objects do not create phantom blank rows, and object columns still follow a `--query` multiselect hash. A top-level scalar list joins into a single Tab-separated row; use `--query` to project into the exact one-record-per-line shape when piping to `nl`/`grep`.
- `--output off` still sends the API request and writes nothing to stdout. It skips response-dependent `--query` evaluation, but the expression's syntax, function calls, and exact-number safety rules are still validated before the request.
- **`--query` errors are caught before the request**: syntax errors, unknown function names (`lenght(@)`), wrong argument counts (`length(@, @)`), incomplete expressions (`a | [0`), and queries rejected by the exact-number safety rules are reported before the API is called. The message includes the original expression, a `^` marker under the failure, and an actionable hint; a misspelled function name suggests the closest builtin. Non-ASCII field names must be double-quoted, as in `--query '"实例列表"."数据"'`.
- A query that passes preflight can still fail while evaluating the actual response, for example `starts_with(Result, 'x')` when `Result` is an object rather than a string. This happens after the API call; the process exits 1 with `API call succeeded but response output failed`. With `--output off`, this response-dependent evaluation is intentionally skipped.
- API failures (for example HTTP 403) print the error on stderr and do not go through `--output` / `--query`.
- **Exact-number query safety**: field selection, indexing, and structural projection preserve every selected JSON number's original `json.Number` token. Field-to-field response equality and inequality use exact JSON numeric values, so equivalent spellings such as `1` / `1.0`, `1e3` / `1000`, and `-0` / `0` compare equal without changing projected output. Numeric expressions proven not to consume response numbers are also supported, including `abs(length(Items))` and `to_number('42')`; safe-derived numbers may be compared with one another, as in `length(A) > length(B)`. A safe-derived number cannot be compared directly with a response field, because mixing its `float64` result with an exact response number would silently produce the wrong equality result. Ordering or numeric calculation/conversion that may consume a response `json.Number` remains rejected instead of silently rounding it. Backtick JSON literals containing numbers are rejected because the bundled JMESPath parser converts their numbers to `float64`; the only exception is a correctly typed direct sole argument to a shape-only function: `length()` accepts a string, array, or object, while `keys()` accepts only an object (for example, ``length(`[9007199254740993]`)`` or ``keys(`{"N":9007199254740993}`)``). String and array membership with `contains` remains available (for example, `contains(Result.Name, 'web')` and `contains(Result.Tags, 'web')`); a numeric backtick literal such as ``contains(Result.Numbers, `9007199254740993`)`` is rejected. `sort` / `max` / `min`, string-key `sort_by` / `max_by` / `min_by`, and `type()` remain available; if the selected response value is an exact number rather than a supported string value, evaluation fails explicitly after the API response. Use an exact-number-aware JSON tool downstream for operations that directly order or calculate with response numbers.
- **YAML numbers**: `--output yaml` emits response integers as `!!int` scalars and decimal/exponent numbers as `!!float` scalars while preserving the original JSON numeric token, including very large integers, long decimals, exponent spelling, and trailing zeros. Numbers are not silently rounded and long decimals are not converted to strings. YAML object keys are sorted alphabetically. The yaml.v3 encoder may indent sequences differently from earlier releases; this is a presentation-only change and the parsed YAML data is equivalent. Do not depend on byte-for-byte YAML whitespace. `--query` write order affects `table` / `table-num` / `text` columns, not YAML key order.

---

[← Configuration](3-Configuration.md) | Usage[(中文)](4-Usage-zh.md) | [Advanced Usage →](5-Advanced.md)
