[← Getting Started](1-GettingStarted.md) | Authentication[(中文)](2-Authentication-zh.md) | [Configuration →](3-Configuration.md)

---

## Authentication

Volcengine CLI supports config profiles, the environment-based default credential chain, SSO, and Console Login. Authentication configuration is stored in `~/.volcengine/config.json`.

## Credential Resolution Priority

When a service command creates an SDK client, credentials and runtime settings are resolved in this order:

1. `---profile`: applies only to the current invocation and must reference an existing profile.
2. The `current` profile in the config file.
3. The profile named by `VOLCENGINE_PROFILE` or `VOLCSTACK_PROFILE`.
4. The SDK default credential chain: environment variables, OIDC, CLI config provider, ECS instance role, and other SDK providers.

Region priority:

1. `---region`
2. `region` in the profile
3. `VOLCENGINE_REGION`

Endpoint priority:

1. `---endpoint`
2. `endpoint` in the profile
3. `VOLCENGINE_ENDPOINT`

When `endpoint-resolver` or `VOLCENGINE_ENDPOINT_RESOLVER` is `standard`, the SDK standard endpoint resolver is used and explicit endpoint is ignored. Setting endpoint to `auto-addressing` also enables the standard endpoint resolver.

## Credential Modes

| Mode | Purpose | Required fields |
| --- | --- | --- |
| `ak` | Static AK/SK credentials, the default mode | `access-key`, `secret-key` |
| `sso` | Single sign-on | configured with `ve configure sso` |
| `console-login` | Console OAuth login with temporary STS credentials | written by `ve login` |
| `ramrolearn` | AssumeRole via STS with AK/SK | `access-key`, `secret-key`, `role-name`, `account-id` |
| `oidc` | Exchange an OIDC token for temporary credentials | `oidc-token-file`, `role-trn` |
| `ecsrole` | ECS instance role through IMDS | `role-name` |

`ve configure set` validates required fields for the selected mode. When updating an existing profile, omitted fields keep their previous values. Creating or updating a profile makes it the current profile. `ve configure sso` is the exception: it writes an SSO profile but does not switch the current profile.

## Configure Credentials with Profiles

### AK/SK

```shell
ve configure set --profile prod --mode ak --region cn-beijing --access-key AK --secret-key SK
```

`--mode ak` can be omitted:

```shell
ve configure set --profile prod --region cn-beijing --access-key AK --secret-key SK
```

Temporary credentials can include a session token:

```shell
ve configure set --profile sts-dev --region cn-beijing \
  --access-key AK --secret-key SK --session-token SESSION_TOKEN
```

### AssumeRole

```shell
ve configure set --profile ram-dev --mode ramrolearn --region cn-beijing \
  --access-key AK --secret-key SK \
  --role-name YourRoleName --account-id 2100000000
```

### OIDC

```shell
ve configure set --profile ci-oidc --mode oidc --region cn-beijing \
  --oidc-token-file /var/run/secrets/oidc-token \
  --role-trn trn:iam::2100000000:role/CIRole
```

### ECS Instance Role

```shell
ve configure set --profile ecs-role --mode ecsrole --region cn-beijing \
  --role-name YourEcsRoleName
```

## Profile Fields

```shell
profile: Profile name. Required when creating or updating a profile.
mode: Credential mode. One of ak, sso, console-login, ramrolearn, oidc, ecsrole. New profiles default to ak when omitted.
access-key: Access Key.
secret-key: Secret Key.
session-token: Temporary credential session token.
region: API region. Optional during configure set, but required by API calls through profile, ---region, or VOLCENGINE_REGION.
endpoint: Custom endpoint. Ignored when endpoint-resolver is standard.
endpoint-resolver: Set to standard to use the standard endpoint resolver.
http-proxy: HTTP proxy used by the SDK when SSL is disabled.
https-proxy: HTTPS proxy used by the SDK.
disable-ssl: Whether to disable SSL. Written only when explicitly provided.
use-dual-stack: Whether to enable dual-stack endpoints. Written only when explicitly provided.
role-name: Required for ramrolearn and ecsrole.
account-id: Required for ramrolearn.
oidc-token-file: Required for oidc.
role-trn: Required for oidc.
login-session: console-login field written by ve login. Do not configure it manually.
sso-session: sso field written by ve configure sso.
```

## Use Environment Variables

If no usable profile is active, the CLI uses the SDK default credential chain. The most common setup is AK/SK environment variables:

```shell
export VOLCENGINE_ACCESS_KEY=AK
export VOLCENGINE_SECRET_KEY=SK
export VOLCENGINE_REGION=cn-beijing

# Optional: temporary credentials
export VOLCENGINE_SESSION_TOKEN=SESSION_TOKEN

# Optional: endpoint settings
export VOLCENGINE_ENDPOINT=open.volcengineapi.com
export VOLCENGINE_ENDPOINT_RESOLVER=standard

# Optional: network settings
export VOLCENGINE_DISABLE_SSL=false
export VOLCENGINE_USE_DUALSTACK=false
```

OIDC environment variables:

```shell
export VOLCENGINE_OIDC_TOKEN_FILE=/path/to/oidc/token
export VOLCENGINE_OIDC_ROLE_TRN=trn:iam::2100000000:role/YourRoleName
export VOLCENGINE_REGION=cn-beijing
```

To ensure only explicit profiles are used, disable the default credential chain:

```shell
export VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS=true
```

When this is set and no active profile exists, the CLI returns an error instead of trying environment variables or IMDS.

## SSO Login

SSO uses two layers:

- `sso-session`: enterprise SSO entry point with Start URL, Region, and Scopes.
- SSO profile: an account and role binding with `mode=sso`, `sso-session-name`, `account-id`, `role-name`, `region`, and related fields.

### Quick Start

```shell
# 1. Create an SSO session. registration-scopes can be omitted
ve configure sso-session --name my-sso \
  --start-url https://{custom}.volccloudidentity.com/userportal \
  --region cn-beijing

# 2. Create an SSO profile, authorize with device code, and select account and role
ve configure sso --profile my-dev --sso-session my-sso

# 3. Switch the current default profile
ve configure profile --profile my-dev

# 4. Call APIs with that profile
ve sts GetCallerIdentity
```

`ve configure sso` does not switch the current profile. If you skip step 3, service commands keep using the previous current profile.

### Command Relationships

| Command | When to use it | What it does | Switches current |
| --- | --- | --- | --- |
| `ve configure sso-session` | Usually once per SSO entry point | Stores Start URL, Region, and Scopes; reusable by multiple SSO profiles | No |
| `ve configure sso` | Once per account + role combination | Links an SSO session, performs first authorization, selects account and role, writes an SSO profile | No |
| `ve configure profile --profile NAME` | When service commands should use a profile by default | Switches current profile | Yes |
| `ve sso login` | When prompted to log in again, or to refresh SSO login state explicitly | Runs device authorization again and caches access token | No |
| `ve sso logout` | To log out one or all SSO sessions | Revokes cached tokens, removes token cache, clears STS temporary credentials | No |

### Configure SSO Session

```shell
ve configure sso-session --name my-sso \
  --start-url https://{custom}.volccloudidentity.com/userportal \
  --region cn-beijing \
  --registration-scopes cloudidentity:account:access,offline_access
```

Parameters:

```shell
name: SSO session name. Omit it to enter interactive selection/creation mode.
start-url: SSO Start URL, usually your sign-in URL with the /userportal suffix.
region: SSO region. Defaults to cn-beijing.
registration-scopes: Comma-separated scope list. Defaults to cloudidentity:account:access,offline_access.
```

Scopes can only be `cloudidentity:account:access` and `offline_access`. The CLI trims, deduplicates, and validates them. When editing an existing session, Start URL, Region, and Scopes are prefilled; press Enter to keep the current value.

### Configure SSO Profile

```shell
ve configure sso --profile my-dev --sso-session my-sso
```

For servers without a GUI:

```shell
ve configure sso --profile my-dev --sso-session my-sso --no-browser
```

If `--profile` is empty, the interactive flow lets you press Enter and defaults to `{sso-role-name}-{sso-account-id}`. If the named `--sso-session` does not exist, the command guides you through creating it.

### Daily Auto-Refresh

When the current profile is an SSO profile, service commands automatically check and refresh STS temporary credentials:

- Reuse `session-token` when it has not expired.
- If STS credentials are missing or expired, use cached SSO access token plus `account-id` / `role-name` to request new STS credentials and write them back to the profile.
- If the SSO access token is expired or close to expiry, only a silent refresh with refresh token is attempted. Service commands do not automatically open a browser.
- If cache is missing, refresh token is missing, client registration expired, or refresh fails, the command asks you to run `ve sso login`.

### SSO Login

```shell
ve sso login --profile my-dev
ve sso login --sso-session my-sso
ve sso login
```

`ve sso login` explicitly logs in again. Each run performs device authorization and does not silently exchange an existing refresh token for access token.

Options:

```shell
--profile: SSO profile to use. It must exist, be mode sso, and have sso-session configured.
--sso-session: SSO session to use. It must exist and be valid.
--no-browser: Disable automatically opening the browser.
```

If neither `--profile` nor `--sso-session` is provided: no session returns an error; one session is used directly; multiple sessions open a searchable selection list.

### SSO Logout

```shell
ve sso logout --sso-session my-sso
ve sso logout
```

Without a session name: no session returns an error; one session is logged out directly; multiple sessions open a selection list that also includes “All SSO sessions”.

Logout does:

- Revoke cached refresh token for the SSO session.
- Delete the token cache for the SSO session.
- Clear `access-key`, `secret-key`, `session-token`, and `sts-expiration` from linked SSO profiles.

Logout does not delete SSO profiles, delete sso-session configuration, or clear `account-id` / `role-name`.

## Console Login

Console Login uses Volcengine Console OAuth 2.0 + PKCE and caches temporary STS credentials locally.

```shell
# Log in with the default profile. If region is omitted, the CLI prompts for it
ve login

# Specify profile and region
ve login -p dev -r cn-beijing

# Use cross-device login for headless servers or containers
ve login -p dev -r cn-beijing --remote
```

Options:

```shell
--profile, -p: Profile name. Defaults to default.
--region, -r: Region. When omitted, the CLI prompts and uses cn-beijing if you press Enter.
--remote: Cross-device login. Open the printed URL in a browser and paste the authorization code back into the terminal.
--endpoint-url: Sign-in service endpoint. Defaults to https://signin.volcengine.com and normally does not need changes.
```

After login, the profile is written as `console-login` mode with a `login-session`. Logging into a non-`default` profile does not switch active profile automatically:

```shell
ve configure profile --profile dev
```

End-to-end flow:

```shell
ve login --profile dev --region cn-beijing
ve configure profile --profile dev
ve sts GetCallerIdentity
ve logout --profile dev
```

## Console Logout

```shell
# Log out default profile
ve logout

# Log out a specific profile
ve logout -p dev

# Log out all console-login profiles in the current config
ve logout --all
```

`ve logout` only clears local login state: it removes cached credentials and clears `login-session` from the profile. It does not delete the profile or send a server-side logout request.

Notes:

- Without `--profile`, only `default` is handled. The command does not automatically use current.
- Only `console-login` profiles are affected. AK and SSO profiles are not affected.
- `--all` ignores `--profile` and clears all `console-login` profiles.

## FAQ

**Service commands still use the old account after `ve configure sso`. What should I do?**

Run `ve configure profile --profile NAME` to switch current. `configure sso` writes a profile but does not switch current.

**When do I need `ve sso login`?**

The first `ve configure sso` already authorizes. Daily service commands reuse or silently refresh credentials. Run `ve sso login` only when prompted or when you want to explicitly refresh SSO login state.

**How do I log in on a machine without a graphical browser?**

Use `--no-browser` for SSO and `--remote` for Console Login.

**What should I enter for Scopes?**

Usually nothing. The default is `cloudidentity:account:access,offline_access`.

---

[← Getting Started](1-GettingStarted.md) | Authentication[(中文)](2-Authentication-zh.md) | [Configuration →](3-Configuration.md)
