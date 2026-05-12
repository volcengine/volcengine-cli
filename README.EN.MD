## Volcengine Command Line Tools

[中文](README.MD) | English

### Special Note

To make it easier for users, starting from version v1.0.20, the command prefix for the Volcengine CLI tool has been changed from 'volcengine-cli' to 've'. Earlier versions are not affected. Users who have upgraded to v1.0.20 or later should promptly update the command prefix (refer to the alias command for unified configuration).

### Overview

1.  VolcEngine Command Line Tool
2.  The minimum required Go version is 1.5 or higher; version 1.12 or above is recommended.

### Install VolcEngine CLI

#### Obtain the client from the release package.

1.  <https://github.com/volcengine/volcengine-cli/releases> Get the latest version
2.  Download the version corresponding to your operating system and extract it for use.

#### Obtaining the client by compiling from source

1.  Compile the client using build.sh.
```shell
# If using a Mac 
sh build.sh darwin
mv ve-darwin ve
# If Using Windows
sh build.sh windows
mv ve-windows ve
# If using Linux
sh build.sh linux
mv ve-linux ve
```


### Configuring VolcEngine CLI

Add the Volcano Engine CLI to the environment variables.

1.  Check whether /usr/local/bin exists in the $PATH system variable. If it does not, please set a usable environment variable for the Volcano Engine CLI according to your actual situation.

2.  Execute the following command to copy 've' to the /usr/local/bin directory, after which it will be available for use.

    ```shell
    sudo cp ve /usr/local/bin
    ```

#### Configure Credentials

Volcengine CLI supports multiple credential modes and resolves credentials in the following order:

1. Active profile in the configuration file: if an active profile exists, the CLI uses the credentials stored in that profile.
2. Default credential chain: if there is no active profile, the CLI tries credentials in this order:
   - Environment variables (`VOLCENGINE_ACCESS_KEY` / `VOLCENGINE_SECRET_KEY`)
   - OIDC environment variables (`VOLCENGINE_OIDC_TOKEN_FILE` + `VOLCENGINE_OIDC_ROLE_TRN`)
   - ECS instance role from IMDS
3. If all methods fail, the CLI returns an error.

Credential modes:

| Mode | Description | Required fields in profile |
|------|-------------|----------------------------|
| `ak` | Static AK/SK credentials (default) | `access-key`, `secret-key` |
| `ststoken` | AK/SK with temporary session token | `access-key`, `secret-key`, `session-token` |
| `sso` | SSO sign-in | configured through `ve configure sso` |
| `ramrolearn` | AssumeRole via STS | `access-key`, `secret-key`, `role-name`, `account-id` |
| `oidc` | OIDC token exchange | `oidc-token-file`, `role-trn` |
| `ecsrole` | ECS instance role via IMDS | `role-name` |

When you configure a profile with `ve configure set`, all required fields for that mode must be written into the profile itself. Environment variables are only used by the default credential chain when no profile is active.

1.  Example of configuration using 've configure set'

    ```shell
    ve configure set --profile test --region cn-beijing --access-key ak --secret-key sk
    ```

    Supported Parameters and Related Descriptions

    ```shell
	profile: Configuration name. If the profile already exists, this command will modify the existing configuration; otherwise, it will create a new configuration and set the currently used profile to the new configuration. 
	mode: Credential mode. Supported values: ak, ststoken, sso, ramrolearn, oidc, ecsrole. Default is ak.
	access-key: Your Access Key 
	secret-key: Your SK 
	region: Geographic area, such as cn-beijing 
	session-token: Required for ststoken mode.
	role-name: Required for ramrolearn and ecsrole modes.
	account-id: Required for ramrolearn mode.
	oidc-token-file: Path to the OIDC token file. Required for oidc mode.
	role-trn: Role TRN. Required for oidc mode.
	disable-ssl: Specifies whether to disable SSL. The default value is false. 
	endpoint: Optional; the default value is open.volcengineapi.com. Ignored when endpoint-resolver is standard.
    endpoint-resolver: Optional; set to standard (case-insensitive) to use the standard resolver. Otherwise, the CLI uses endpoint when provided.
    use-dual-stack: Optional; enable dual-stack endpoints when true. The default value is false.
    ```

2.  If no configuration is specified in the configuration file, an attempt will be made to read the configuration from environment variables set via export.
    ```shell
	export VOLCENGINE_ACCESS_KEY=AK
	export VOLCENGINE_SECRET_KEY=SK 
	export VOLCENGINE_REGION=cn-bejing 

	# Whether to disable SSL; if not set, the default is false. 
	export VOLCENGINE_DISABLE_SSL=false 

	# Optional: custom endpoint. Ignored when VOLCENGINE_ENDPOINT_RESOLVER is standard.
	export VOLCENGINE_ENDPOINT=open.volcengineapi.com

	# Optional: endpoint resolver; set to standard (case-insensitive) to use the standard resolver.
	export VOLCENGINE_ENDPOINT_RESOLVER=standard

	# Optional: enable dual-stack endpoints; accepts true/false.
	export VOLCENGINE_USE_DUALSTACK=false 

	# If role-playing is employed, the following must be provided
	export VOLCENGINE_SESSION_TOKEN=sessionToken
    ```

    To disable the default credential chain (e.g., for debugging or security-sensitive environments where only explicitly configured profiles should be used):

    ```shell
    export VOLCENGINE_DISABLE_DEFAULT_CREDENTIALS=true
    ```

    When set, the CLI will return an error if no active profile is configured, instead of attempting to resolve credentials from environment variables or IMDS.

#### Other Operations of the configure Command

1.  After configuring a profile using the Volcengine CLI 'configure' command, the CLI will prioritize the authentication information within the profile for API request signing.
2.  After a regular AK profile is added or modified with `ve configure set`, the default profile used in the current configuration will be switched to that profile. `ve configure sso` does not automatically switch the current profile; after creating an SSO profile, run `ve configure profile --profile [profile name]` if you want business commands to use it by default.
3.  If multiple profiles exist, use 've configure profile --profile [configuration name]' to switch before invoking the interface to ensure correct usage.

###### Retrieve specified profile configuration information

```shell
ve configure get --profile [profile name]
```

The 'profile' field is optional. If the 'profile' field is not specified, the current configuration will be displayed. If the 'profile' field is specified, the Volcano Engine CLI will attempt to retrieve and display the specified configuration.

If the configuration does not exist, all configuration fields in the returned result will be set to their default values.

###### Display information for all current configuration profiles.

```shell
ve configure list
```

###### Switch the currently used configuration (profile). Please ensure that the version is greater than or equal to 1.0.16.

```shell
ve configure profile --profile [profile name]
```

The 'profile' field is required. When a profile is specified, the Volcengine CLI will attempt to switch to the corresponding configuration.

If the configuration does not exist, the current configuration will not be switched, and an error message will be displayed.

###### Create/Modify Configuration Profile

```shell
ve configure set --profile [Profile Name] --region [Region] --access-key [User's AK] --secret-key [User's SK] --endpoint [Endpoint corresponding to the region]
```

After creating or modifying a configuration, the active configuration will switch to the newly created or modified configuration.

When creating a new configuration, the profile field must be specified; when modifying an existing configuration, the profile field must be specified.

> Note: region is not mandatory during `configure set`, but it must be available at API call time (via profile, `---region` override, or `VOLCENGINE_REGION` environment variable).

Other fields that can be specified:

-   mode
-   access-key
-   secret-key
-   region
-   session-token
-   role-name
-   account-id
-   oidc-token-file
-   role-trn
-   disable-ssl
-   endpoint
-   endpoint-resolver
-   use-dual-stack

For information on the function of each field, please refer to the 'Supported Parameters and Related Descriptions' section in the previous section.

###### Delete Configuration Profile

```shell
ve configure delete --profile [profile name]
```

When deleting a configuration, the profile field must be specified to indicate the name of the configuration to be deleted.

If the configuration to be deleted is currently in use, then after successful deletion, Volcano Engine CLI will attempt to randomly select one of the remaining configurations as the new current configuration.

#### SSO Quick Start

SSO uses two layers of configuration: `sso-session` stores the enterprise SSO entry point (Start URL, Region, and Scopes), while an SSO profile stores the selected account and role binding. For first-time setup, follow this order:

```shell
# 1. Create an SSO session. registration-scopes can be omitted; press Enter at the prompt to use the default
ve configure sso-session --name my-sso --start-url https://{custom}.volccloudidentity.com/userportal --region cn-beijing

# 2. Create an SSO profile, complete device authorization, and select an account and role
ve configure sso --profile my-dev --sso-session my-sso

# 3. Switch the current default profile to the SSO profile you just created
ve configure profile --profile my-dev

# 4. Run business commands normally; the CLI refreshes STS temporary credentials when needed
ve [service] [action] [params]
```

Note: `ve configure sso` writes the SSO profile but does not automatically change the current default profile. If you skip step 3, business commands will continue using the previous current profile.

#### SSO Command Relationships

| Command | When to use it | What it does | Switches current profile |
| --- | --- | --- | --- |
| `ve configure sso-session` | Usually once per SSO entry point | Stores Start URL, Region, and Scopes; reusable by multiple SSO profiles | No |
| `ve configure sso` | Once per account + role combination | Links an SSO session, performs first authorization, selects account and role, and writes an SSO profile | No |
| `ve configure profile --profile [profile name]` | When business commands should use a profile by default | Switches the current default profile | Yes |
| `ve sso login` | When prompted to log in again, or when you want to refresh the SSO login state explicitly | Runs device authorization again and caches the access token | No |
| `ve sso logout` | When logging out of one or all SSO sessions | Revokes cached tokens, removes token cache, and clears STS temporary credentials | No |

#### Configure SSO Session (configure sso-session)

```shell
ve configure sso-session --name [session name] --start-url [SSO Start URL] --region [region] --registration-scopes [scope1,scope2]
```

This command creates or updates an SSO session for later use with `ve configure sso`. Parameters:

```shell
name: SSO session name; if omitted, enter interactive selection/creation mode
start-url: SSO Start URL; required. Format: your sign-in URL with the /userportal suffix, for example https://{custom}.volccloudidentity.com/userportal. When editing an existing session, press Enter to keep the default
region: SSO region; default is cn-beijing. When editing an existing session, press Enter to keep the default
registration-scopes: SSO scope list (comma-separated); optional, default cloudidentity:account:access,offline_access; allowed values are only cloudidentity:account:access and offline_access
```

Interactive flow notes:
- If no sessions exist, you will be prompted to enter a non-empty session name
- If sessions exist, you get a searchable list with “<Create new session>” to add one
- When editing, Start URL, Region, and Scopes are pre-filled; press Enter to keep defaults
- Scopes are trimmed, deduplicated, and validated; empty input falls back to the default scopes

#### Configure SSO Profile (configure sso)

```shell
ve configure sso --profile [profile name] --sso-session [session name] [--no-browser]
```

This command creates or updates an SSO profile. It links an SSO session, runs the device authorization flow, lists accounts and roles for selection, and writes `mode=sso`, `sso-session-name`, `account-id`, `role-name`, and `region` to `~/.volcengine/config.json`.

Parameters:

```shell
profile: profile name; if omitted, you can press Enter and the default becomes {sso-role-name}-{sso-account-id}
sso-session: SSO session name; if omitted, enter interactive selection/creation mode
no-browser: add --no-browser to disable automatically opening the browser, useful on servers without a GUI
```

Notes:
- If the profile already exists and is not an SSO profile, the command refuses to overwrite it
- If the specified SSO session does not exist, you will be guided to create it and enter Start URL, Region, and Scopes
- `configure sso` completes first authorization and account/role selection, but does not switch the current default profile; run `ve configure profile --profile [profile name]` when needed
- To switch accounts or roles, run `ve configure sso --profile [profile name] --sso-session [session name]` again

#### Daily Login, Auto-Refresh, and Logout

##### Business Command Auto-Refresh

When the current default profile is an SSO profile, business commands automatically check and refresh STS temporary credentials:
- If the profile's `session-token` has not expired, it is reused
- If STS credentials are missing or expired, the CLI uses the cached SSO access token plus the profile's `account-id` / `role-name` to request new STS credentials and writes them back to the profile
- If the SSO access token is expired or close to expiry, the CLI only attempts a silent refresh with refresh_token; business commands do not automatically open a browser
- If no access token is cached, refresh_token is missing, client registration has expired, or refresh fails, the business command asks you to run `ve sso login`

##### SSO Login (sso login)

```shell
ve sso login --profile [profile name]
# or
ve sso login --sso-session [session name]
# or omit arguments and let the CLI auto-select or prompt from configured sso-sessions
ve sso login
```

`ve sso login` explicitly logs in to SSO again. It runs device authorization and caches a new access token. In line with AWS SSO login semantics, each execution re-authenticates the current SSO session and does not silently exchange an existing refresh_token for an access token.

Parameters:

```shell
profile: the SSO profile to use; must exist, be of sso type, and have sso-session configured
sso-session: the SSO session to use; the session must exist and be valid
no-browser: add --no-browser to disable automatically opening the browser
```

Login behavior:
- If neither profile nor sso-session is provided: error when no sessions are configured; use the only session if one exists; otherwise enter interactive selection with search
- `sso login` does not reselect account/role; account and role come from `account-id` / `role-name` written by `ve configure sso`
- Only the device code flow is supported; use `--no-browser` to disable auto-opening the browser

##### SSO Logout (sso logout)

```shell
ve sso logout --sso-session [session name]
# or omit arguments and let the CLI auto-select or prompt from configured sso-sessions
ve sso logout
```

`ve sso logout` logs out of the SSO login state. With `--sso-session`, it logs out of that session. Without it, the command errors when no sessions are configured, logs out of the only session when exactly one exists, or opens a searchable selection list when multiple sessions exist. The selection list also includes “All SSO sessions” for batch logout.

Logout does:
- Revoke the cached refresh token for the SSO session, if present
- Delete the SSO session token cache
- Clear STS temporary credential fields from linked SSO profiles: `access-key`, `secret-key`, `session-token`, `sts-expiration`

Logout does not:
- Delete SSO profiles
- Delete sso-session configuration
- Clear `account-id` / `role-name` from profiles

After logout, run `ve sso login` to keep using the same account and role. Run `ve configure sso` again only when switching accounts or roles.

#### SSO FAQ

- Business commands still use the old account after `ve configure sso`. What should I do?
  Run `ve configure profile --profile [profile name]` to switch the current default profile. `configure sso` writes a profile but does not automatically change the current profile.

- When do I need to run `ve sso login`?
  The first `ve configure sso` already completes authorization. Daily business commands reuse or silently refresh credentials. Run `ve sso login` only when the CLI asks you to log in again, or when you want to refresh the SSO login state explicitly.

- Why does `ve sso login` open the authorization flow every time?
  This is explicit login behavior: each run performs device authorization again and does not silently complete login with an existing refresh_token.

- How do I switch accounts or roles?
  Run `ve configure sso --profile [profile name] --sso-session [session name]` again, then select the account and role from the interactive lists.

- How do I log in on a machine without a graphical browser?
  Add `--no-browser` to `ve configure sso` or `ve sso login`, then complete authorization in a browser using the URL printed by the CLI.

- What should I enter for Scopes?
  Usually nothing. The default is `cloudidentity:account:access,offline_access`. If you enter scopes manually, only `cloudidentity:account:access` and `offline_access` are allowed; the CLI deduplicates and validates them.

#### Console Login (login)

```shell
ve login --profile [profile name]
# Specify a region to avoid the interactive prompt
ve login --profile [profile name] --region cn-beijing
# or cross-device login
ve login --profile [profile name] --region cn-beijing --remote
```

This command signs in through the Volcengine Console using OAuth 2.0 + PKCE and caches temporary STS credentials locally for subsequent CLI calls. Parameters:

```shell
profile: configuration profile name; optional; defaults to default
region: optional; when omitted, the CLI prompts for it and defaults to cn-beijing when you press Enter
remote: enables cross-device login mode; when set, the CLI prints a login URL and prompts for the authorization code shown in the browser
endpoint-url: optional; overrides the sign-in service endpoint; defaults to https://signin.volcengine.com
```

Login behavior:
- The default mode is local login: the CLI starts a local callback server, prints an authorization URL, tries to open your browser automatically, and waits for the browser redirect after authentication. If the browser does not open, visit the URL printed in the terminal manually
- If `--region` is omitted, the CLI prompts with `Please enter region [cn-beijing]: `. Press Enter to use `cn-beijing`; for scripts and automation, pass `--region` explicitly
- With `--remote`, the CLI uses cross-device login: this is suitable for headless environments or machines that cannot open a browser locally. The CLI prints a login URL, you complete authentication on any device, and then paste the complete base64 authorization code shown in the browser back into the terminal
- After a successful login, the CLI stores the profile with `mode` set to `console-login` and records the `login-session`; token cache files are stored under `~/.volcengine/login/cache/`
- You can override the login cache directory with the `VOLCENGINE_LOGIN_CACHE_DIRECTORY` environment variable, which is useful for multi-user machines or CI isolation
- If the target profile already exists, the command reuses and overwrites that profile, updating its `mode` to `console-login`
- If the profile is already bound to a different `login-session`, the CLI prompts before replacing it

Usage notes:
- This is useful when you want temporary credentials obtained from your console identity without manually preparing AK/SK
- If the local session expires and a refresh token is available in cache, the CLI will try to refresh it automatically; if refresh fails, run `ve login` again
- To isolate different console identities, use separate `--profile` values

End-to-end example:

```shell
# 1. Sign in and write the dev profile
ve login --profile dev --region cn-beijing

# 2. If dev is not the current default profile, switch the active profile first
ve configure profile --profile dev

# 3. Service commands use the console-login temporary credentials from the current profile
ve ecs DescribeInstances

# 4. When finished, clear the local login state
ve logout --profile dev
```

Notes:
- On the first login, if `current` is empty in the configuration file, the CLI sets the logged-in profile as the current default profile automatically. If another current profile already exists, switch manually with `ve configure profile --profile [profile name]`
- When subsequent service commands use this profile, the CLI reads temporary STS credentials from the local cache. If the credentials are close to expiration and a refresh token exists, the CLI refreshes them automatically
- `ve logout` only clears the local cache and the `login-session` in the configuration; it does not delete the whole profile

#### Console Logout (logout)

```shell
ve logout --profile [profile name]
# or clear all login credentials
ve logout --all
```

This command logs out of a console login session established via `ve login` and removes the locally cached temporary STS credentials. This is a purely local operation — no network requests are made to any server. Parameters:

```shell
profile: configuration profile name; optional; defaults to default
all: remove cached login credentials for all console-login profiles; when set, --profile is ignored
```

Logout behavior:
- The default mode logs out a single profile: the CLI locates and removes the cached token file under `~/.volcengine/login/cache/` based on the `login-session` recorded in the profile, and clears the `login-session` field in the configuration
- With `--all`, the CLI iterates all profiles in the configuration, identifies those using `console-login` mode with an active `login-session`, removes the corresponding cache file for each via SHA-1 lookup, and clears the `login-session` field. Failure to clean one profile does not affect the others
- Only profiles using `console-login` mode can be logged out with this command; the command returns an error for AK or SSO profiles
- If the profile does not have an active login session (`login-session` is empty), the command prints a message and exits normally
- `ve logout` is a purely local cleanup operation and does not send a logout request to the server. After logout, credentials that have already been loaded by other tools may remain valid until they expire (typically within 15 minutes)
- Logout only clears the local token cache and the profile's `login-session`; it does not delete the whole profile or clear other fields such as `region`

Usage notes:
- On shared devices, it is recommended to run `ve logout` after you finish using the CLI
- To thoroughly clean all login caches (e.g., for troubleshooting or device handover), use `ve logout --all`
- After logout, run `ve login` to re-establish a login session when needed

#### Configure Auto-Completion

By executing ve completion --help, users can view the methods for configuring auto-completion in various terminals. Based on the instructions provided, users may choose whether to configure the auto-completion feature.

<br/>

##### Bash

The Bash completion script for Volcano Engine CLI can be viewed using 've completion bash'. To enable the autocompletion feature, import this script into your shell.

###### 1. Install bash-completion

The Volcengine CLI completion script depends on the bash-completion tool. Therefore, you must install and enable bash-completion in advance. You can use the command 'type _init_completion' to verify whether bash-completion is installed.

1.  Installation: yum install bash-completion or apt-get install bash-completion

2.  Enable bash-completion: source /usr/share/bash-completion/bash_completion

    It is recommended to add 'source /usr/share/bash-completion/bash_completion' to your ~/.bashrc file.

3.  Verify whether bash-completion is installed successfully: execute type _init_completion to check the installation status of bash-completion.

###### 2. Configuring Auto-Completion

Follow these steps to enable the auto-completion feature in Bash:

1.  echo 'source <(ve completion bash)' >> ~/.bashrc
2.  ve completion bash > /etc/bash_completion.d/ve

The changes will take effect after reloading the Shell or executing 'source ~/.bashrc'.

If the error '_get_comp_words_by_ref: command not found' occurs, please verify again that bash-completion has been installed and configured correctly.

<br/>

##### Zsh

The Zsh completion script for Volcengine CLI can be viewed by executing 've completion zsh'. To enable the auto-completion feature, import this completion script into your shell.

Configure the auto-completion feature in Zsh by following these steps:

1.  Enabling compinit in Zsh:
	echo "autoload -U compinit; compinit" >> ~/.zshrc
2.  Configure Auto-Completion
	ve completion zsh > "${fpath[1]}/_ve"

The changes will take effect after reloading the shell or executing 'source ~/.zshrc'.

#### Configure Color

The colored display feature can be enabled by using ve enable-color.

The ve disable-color command can be used to disable the color display feature.

### Using Volcengine CLI

#### Query the list of supported services and the list of supported interfaces under each service

1.  Query the list of supported services
    ```shell
    ve [-h|--help]
    ```
2.  List of Supported Interfaces under the Query Service
    ```shell
    ve ecs [-h|--help]
    ```

#### Show CLI version

```shell
ve version
```

```shell
ve -v
```

#### Call the API

When using the VolcEngine CLI to invoke an API, the basic command structure is as follows:

```shell
ve <service name> <action> [--parameter1 value1 --parameter2 value2 ...]
```

You can use ve <service name> <action> --help to view information such as the parameter list and return results for the API you wish to invoke. For example: ve ecs DescribeInstances --help.

For more detailed information, you may also refer to <https://www.volcengine.com/docs>.

<br/>

The following code examples are provided:

-   Basic Usage

    ```shell
    ve ecs DescribeInstances
    ```

    ```shell
    ve rds_mysql ListDBInstanceIPLists --InstanceId "xxxxxx"
    ```

-   Supports JSON input

    ```shell
    ve rds_mysql ModifyDBInstanceIPList --InstanceId "xxxxxx" --GroupName "xxxxxx" --IPList '["10.20.30.40", "50.60.70.80"]'
    ```

-   For requests with ContentType set to application/json, the Volcengine CLI also supports passing the body directly as JSON data.

    ```shell
    ve rds_mysql ModifyDBInstanceIPList --body '{"InstanceId":"xxxxxx", "GroupName": "xxxxxx", "IPList": ["10.20.30.40", "50.60.70.80"]}'
    ```

## Security and privacy
This project takes security seriously. 
For vulnerability reporting and supported versions, see [SECURITY.md](SECURITY.md)
