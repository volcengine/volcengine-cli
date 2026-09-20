[← Authentication](2-Authentication.md) | Configuration[(中文)](3-Configuration-zh.md) | [Usage →](4-Usage.md)

---

## Configuration

CLI profiles and SSO sessions are stored in `~/.volcengine/config.json` by default. The config file is written with `0600` permissions, and the config directory is created with `0700` permissions.

This document covers profile inspection, switching, updates, and deletion. Credential modes are covered in [Authentication](2-Authentication.md).

## Config File Structure

The config file contains:

- `current`: current default profile name.
- `profiles`: profile map.
- `sso-session`: SSO session map.
- `enableColor`: whether colored JSON output is enabled. See [Advanced Usage](5-Advanced.md).

Example:

```json
{
    "current": "prod",
    "profiles": {
        "prod": {
            "name": "prod",
            "mode": "ak",
            "access-key": "AK",
            "secret-key": "SK",
            "region": "cn-beijing"
        }
    },
    "enableColor": false,
    "sso-session": {}
}
```

Avoid manually editing sensitive fields. Prefer CLI commands.

## Show Current Profile

```shell
ve configure get
```

Without `--profile`, the command shows the current profile:

```shell
no profile name specified, show current profile: [prod]
```

## Show a Specific Profile

```shell
ve configure get --profile prod
```

If the profile does not exist, the command prints an empty profile object and does not create it.

## List All Profiles

```shell
ve configure list
```

The output starts with current:

```shell
*** current profile: prod ***
```

Then each profile in the config file is printed.

## Switch Current Profile

```shell
ve configure profile --profile prod
```

`--profile` is required. If the profile does not exist, current is not changed and an error is returned.

Switching current affects later service commands that do not specify `--profile`. For a single invocation, use:

```shell
ve ecs DescribeInstances --profile prod
```

## Create or Update a Profile

```shell
ve configure set --profile prod --region cn-beijing --access-key AK --secret-key SK
```

Behavior:

- `--profile` is required.
- If the profile does not exist, it is created with default mode `ak`.
- If the profile exists, only non-empty fields provided in this command are updated; omitted fields keep their previous values.
- `--disable-ssl` and `--use-dual-stack` are written only when explicitly provided.
- Successful create or update switches current to that profile.
- `region` is not mandatory during `configure set`, but API calls must be able to resolve a region.

Update region:

```shell
ve configure set --profile prod --region cn-shanghai
```

Update endpoint:

```shell
ve configure set --profile prod --endpoint ecs.cn-beijing.volcengineapi.com
```

Use the standard endpoint resolver:

```shell
ve configure set --profile prod --endpoint-resolver standard
```

Configure proxy:

```shell
ve configure set --profile prod --https-proxy http://127.0.0.1:7890
```

Enable dual-stack:

```shell
ve configure set --profile prod --use-dual-stack
```

Disable SSL:

```shell
ve configure set --profile prod --disable-ssl
```

## Delete a Profile

```shell
ve configure delete --profile prod
```

`--profile` is required. If the deleted profile is current, the CLI selects one remaining profile as the new current. If no profiles remain, current becomes empty.

Deleting a profile does not delete SSO sessions or the global Console Login cache directory. Console Login cache cleanup is covered in [Authentication](2-Authentication.md#console-logout).

## Safety Policy

The safety policy is a local client-side guard for OpenAPI operations issued by `ve`, including normal calls, `--force` calls, and MCP `call_api`. It complements Volcengine IAM and does not replace cloud-side authorization. Delegated `ve arkcli` and `ve tosutil` commands are not intercepted because their arguments are forwarded unchanged.

The policy is disabled by default and stored separately in `~/.volcengine/safety-policy.json` with `0600` permissions. Enable it and add rules with:

```shell
ve configure safety-policy enable
ve configure safety-policy add --pattern '*:Delete*' --action deny
ve configure safety-policy add --pattern '*:Update*' --action confirm
ve configure safety-policy list
```

Actions:

- `allow`: explicitly allow a matching operation. Configure it in the JSON file or through `VOLCENGINE_SAFETY_POLICY_RULES`; the `add` command intentionally accepts only protective actions.
- `deny`: reject the operation locally without sending an API request.
- `confirm`: require explicit approval before sending the request.
- `forbid`: alias for `confirm`.

Rules use `service:action`, are matched case-insensitively, support `*`, and use first-match-wins order. An unmatched operation is allowed. Rules match the command spelling rather than expanding aliases, so specific aliases must be covered separately.

```json
{
  "enabled": true,
  "rules": [
    {"pattern": "ecs:Describe*", "action": "allow"},
    {"pattern": "ecs:*", "action": "deny"}
  ]
}
```

In an interactive terminal, a `confirm` match prompts for `yes`. In a non-interactive environment such as an AI agent or CI job, the first call returns an error and sends no API request. After the user explicitly approves the operation, repeat the same command with `--yes` or `-y`:

```shell
ve ecs UpdateInstance --InstanceId i-xxxxx --yes
```

`--yes` skips only `confirm`; it never bypasses `deny`. MCP clients use `yes: true` on the repeated `call_api` request. See [MCP Server](6-MCP.md#safety-policy).

Environment overrides have higher priority than the file:

| Variable | Description |
| --- | --- |
| `VOLCENGINE_SAFETY_POLICY_ENABLED` | Override `enabled` (`true`/`false`, also Go boolean forms such as `1`/`0`) |
| `VOLCENGINE_SAFETY_POLICY_RULES` | Replace the file rules with comma-separated `pattern=action` entries |
| `VOLCENGINE_SAFETY_SKIP_CONFIRM` | Treat confirmation as approved for this process (`1`, `true`, `yes`, or `on`) |

Examples:

```shell
export VOLCENGINE_SAFETY_POLICY_ENABLED=true
export VOLCENGINE_SAFETY_POLICY_RULES='ecs:Describe*=allow,*:Delete*=deny,*:Update*=confirm'
```

Invalid policy files and invalid environment overrides fail closed: the CLI reports the configuration error and does not send the API request.

## Selection Examples

### Switch Between Environments

```shell
ve configure set --profile dev --region cn-beijing --access-key DEV_AK --secret-key DEV_SK
ve configure set --profile prod --region cn-beijing --access-key PROD_AK --secret-key PROD_SK

ve configure profile --profile dev
ve ecs DescribeInstances

ve configure profile --profile prod
ve ecs DescribeInstances
```

### Override Profile for One Call

```shell
ve configure profile --profile dev
ve ecs DescribeInstances --profile prod
```

This call uses `prod` only for this invocation and does not modify `current`.

### Override Region and Endpoint for One Call

```shell
ve ecs DescribeInstances --region cn-shanghai
ve sts GetCallerIdentity --region cn-beijing --endpoint sts.volcengineapi.com
```

---

[← Authentication](2-Authentication.md) | Configuration[(中文)](3-Configuration-zh.md) | [Usage →](4-Usage.md)
