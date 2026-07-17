package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

// init 注册 sso 根命令及其子命令，并挂载到根命令上。
func init() {
	// 创建 sso 根命令。
	ssoCmd := newSsoRootCmd()

	// 将 login/logout 子命令添加到 sso 命令下。
	ssoCmd.AddCommand(newSsoLoginCmd())
	ssoCmd.AddCommand(newSsoLogoutCmd())

	// 将 sso 命令注册到根命令。
	rootCmd.AddCommand(ssoCmd)
}

// newSsoRootCmd 创建 sso 根命令，提供统一的使用说明模板。
func newSsoRootCmd() *cobra.Command {
	ssoCmd := &cobra.Command{
		Use:   "sso",
		Short: tr("Single sign-on (SSO) related operations"),
		Long:  tr("Manage operations related to single sign-on (SSO), including login, configuration, etc"),
	}

	// 设置自定义 Usage 模板，统一输出格式。
	ssoCmd.SetUsageTemplate(ssoUsageTemplate())

	return ssoCmd
}

// newSsoLoginCmd 创建 sso login 子命令：支持通过 profile 或 sso-session 登录。
func newSsoLoginCmd() *cobra.Command {
	ssoLoginCmd := &cobra.Command{
		Use:   "login",
		Short: tr("Perform SSO login operations"),
		Long: tr(`Login via SSO, obtain the access token and store it in the cache.
This command requires specifying a configured profile, and this profile must be associated with a valid SS-session.
After a successful login, the system will automatically store the access token for subsequent operations.`),
		Example: tr(`  # Login to SSO using the specified profile
  volcengine-cli sso login --profile my-sso-profile
  # Login to SSO using the specified sso-session
  volcengine-cli sso login --sso-session my-sso-session`),
		// 设置自定义 Usage 模板。
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载配置，作为本次登录的参数来源。
			cfg := ctx.config
			if cfg == nil {
				return trErrorf("the configuration file cannot be loaded")
			}

			// 读取 profile 与 sso-session 相关参数。
			profileName := strings.TrimSpace(cmd.Flag("profile").Value.String())
			ssoSessionName := strings.TrimSpace(cmd.Flag("sso-session").Value.String())
			useDeviceCode := true // 目前仅支持设备码登录流程
			noBrowser, err := cmd.Flags().GetBool("no-browser")
			if err != nil {
				return err
			}

			var sso *Sso
			var activeSessionName string

			// 分支 1：通过 profile 登录。
			if profileName != "" {
				profile, ok := cfg.Profiles[profileName]
				if !ok {
					return trErrorf("the specified profile was not found: %s", profileName)
				}

				// 校验 profile 类型与 sso-session 配置。
				if profile.Mode != ModeSSO {
					return trErrorf("the specified profile is not of sso type")
				}
				if strings.TrimSpace(profile.SsoSessionName) == "" {
					return trErrorf("the specified profile does not have sso-session configured")
				}

				// 组装 SSO 登录参数，优先使用 profile 中配置。
				sso = &Sso{
					Profile:        profile,
					SsoSessionName: profile.SsoSessionName,
					Region:         profile.Region,
					UseDeviceCode:  useDeviceCode,
					NoBrowser:      noBrowser,
				}
				activeSessionName = profile.SsoSessionName
			} else if ssoSessionName != "" {
				// 分支 2：通过显式 sso-session 登录。
				ssoSession, ok := cfg.SsoSession[ssoSessionName]
				if !ok {
					return trErrorf("the specified sso-session was not found: %s", ssoSessionName)
				}
				if ssoSession == nil {
					return trErrorf("the specified sso-session is invalid: %s", ssoSessionName)
				}

				// 使用 sso-session 中的 StartURL/Region 进行登录。
				sso = &Sso{
					SsoSessionName: ssoSessionName,
					StartURL:       ssoSession.StartURL,
					Region:         ssoSession.Region,
					UseDeviceCode:  useDeviceCode,
					NoBrowser:      noBrowser,
				}
				activeSessionName = ssoSessionName
			} else {
				// 分支 3：未指定 profile 或 sso-session 时，根据配置自动选择。
				if len(cfg.SsoSession) == 0 {
					return trErrorf("no sso-session configured")
				}
				// 若仅有一个 sso-session，则直接使用该会话。
				if len(cfg.SsoSession) == 1 {
					for name, session := range cfg.SsoSession {
						if session == nil {
							return trErrorf("the specified sso-session is invalid: %s", name)
						}
						sso = &Sso{
							SsoSessionName: name,
							StartURL:       session.StartURL,
							Region:         session.Region,
							UseDeviceCode:  useDeviceCode,
							NoBrowser:      noBrowser,
						}
						activeSessionName = name
						break
					}
				} else {
					// 多个会话时进入交互式选择。
					options := buildSessionOptions(cfg.SsoSession)
					selectedName, selectedSession, err := selectExistingSession(options)
					if err != nil {
						return err
					}
					if selectedSession == nil {
						return trErrorf("the specified sso-session is invalid: %s", selectedName)
					}
					sso = &Sso{
						SsoSessionName: selectedName,
						StartURL:       selectedSession.StartURL,
						Region:         selectedSession.Region,
						UseDeviceCode:  useDeviceCode,
						NoBrowser:      noBrowser,
					}
					activeSessionName = selectedName
				}
			}

			// 执行登录流程，并输出结果。
			if err := sso.Login(); err != nil {
				if activeSessionName != "" {
					fmt.Printf(tr("login failed for sso-session [%s]: %v\n"), activeSessionName, err)
				}
				return err
			}

			if activeSessionName != "" {
				fmt.Printf(tr("login successfully for sso-session [%s]\n"), activeSessionName)
			} else {
				fmt.Println(tr("login successfully"))
			}
			return nil
		},
	}
	// 添加 profile/sso-session 以及登录行为控制参数。
	ssoLoginCmd.Flags().String("profile", "", tr("Specify the name of the configuration file to be used"))
	ssoLoginCmd.Flags().String("sso-session", "", tr("Specify the SSO session to use when no profile is provided"))
	ssoLoginCmd.Flags().Bool("no-browser", false, tr("Do not automatically open the browser during device authorization"))

	// 设置自定义 Usage 模板。
	ssoLoginCmd.SetUsageTemplate(ssoUsageTemplate())

	return ssoLoginCmd
}

// selectExistingSession 基于已存在会话进行交互式选择。
// 返回所选会话名称与对象，若用户取消则返回错误。
func selectExistingSession(options []sessionOption) (string, *SsoSession, error) {
	if len(options) == 0 {
		return "", nil, trErrorf("no sso-session configured")
	}

	// 支持按名称/区域/URL/Scopes 的模糊搜索。
	searcher := func(input string, index int) bool {
		if index < 0 || index >= len(options) {
			return false
		}
		rawInput := strings.TrimSpace(input)
		lowerInput := strings.ToLower(rawInput)
		item := options[index]
		content := strings.ToLower(item.Name)
		if item.Session != nil {
			content += " " + strings.ToLower(item.Session.Region) + " " + strings.ToLower(item.Session.StartURL) + " " + strings.ToLower(strings.Join(item.Session.RegistrationScopes, ","))
		}
		if lowerInput == "" {
			return true
		}
		return strings.Contains(content, lowerInput)
	}

	// 自定义选择器模板，展示会话摘要与详情信息。
	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "> {{ .Name | cyan }}   {{ sessionRegion .Session }}   {{ sessionStart .Session }}",
		Inactive: "  {{ .Name | faint }}   {{ sessionRegion .Session }}   {{ sessionStart .Session }}",
		Selected: "[*] {{ .Name }}",
		Details: tr(`
--------- SSO Session ----------
Name:   {{ .Name }}
Region: {{ sessionRegion .Session }}
URL:    {{ sessionStart .Session }}
Scopes: {{ sessionScopes .Session }}`),
		FuncMap: buildPromptFuncMap(),
	}

	// 启动交互选择器，允许搜索并回车确认。
	sel := promptui.Select{
		Label:             tr("Select SSO session (type to filter, Enter to choose)"),
		Items:             options,
		Searcher:          searcher,
		Templates:         templates,
		StartInSearchMode: true,
		Size:              10,
	}

	idx, _, err := sel.Run()
	if err != nil {
		return "", nil, err
	}

	// 返回所选会话。
	chosen := options[idx]
	return chosen.Name, chosen.Session, nil
}

var allSessionsLabel = tr("All SSO sessions")

// selectSessionOrAll 在交互式选择中支持单个会话或“全部会话”。
// 返回：会话名称、会话对象、是否选择了全部会话、错误。
func selectSessionOrAll(options []sessionOption) (string, *SsoSession, bool, error) {
	if len(options) == 0 {
		return "", nil, false, trErrorf("no sso-session configured")
	}

	// 追加 “全部会话” 作为可选项。
	choices := make([]sessionOption, 0, len(options)+1)
	choices = append(choices, options...)
	choices = append(choices, sessionOption{Name: allSessionsLabel, Session: nil})

	// 搜索逻辑同样支持名称/区域/URL/Scopes，且“全部会话”始终可见。
	searcher := func(input string, index int) bool {
		if index < 0 || index >= len(choices) {
			return false
		}
		rawInput := strings.TrimSpace(input)
		lowerInput := strings.ToLower(rawInput)
		item := choices[index]
		if item.Name == allSessionsLabel {
			return true
		}
		content := strings.ToLower(item.Name)
		if item.Session != nil {
			content += " " + strings.ToLower(item.Session.Region) + " " + strings.ToLower(item.Session.StartURL) + " " + strings.ToLower(strings.Join(item.Session.RegistrationScopes, ","))
		}
		if lowerInput == "" {
			return true
		}
		return strings.Contains(content, lowerInput)
	}

	// 自定义模板：对“全部会话”使用不同颜色标识。
	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "{{if isAll .}}> {{ .Name | yellow }}{{else}}> {{ .Name | cyan }}   {{ sessionRegion .Session }}   {{ sessionStart .Session }}{{end}}",
		Inactive: "{{if isAll .}}  {{ .Name | faint }}{{else}}  {{ .Name | faint }}   {{ sessionRegion .Session }}   {{ sessionStart .Session }}{{end}}",
		Selected: "[*] {{ .Name }}",
		Details: tr(`
--------- SSO Session ----------
Name:   {{ .Name }}
Region: {{ sessionRegion .Session }}
URL:    {{ sessionStart .Session }}
Scopes: {{ sessionScopes .Session }}`),
		FuncMap: func() map[string]interface{} {
			fm := buildPromptFuncMap()
			fm["isAll"] = func(opt sessionOption) bool {
				return opt.Name == allSessionsLabel
			}
			return fm
		}(),
	}

	// 启动交互选择器，返回用户选择。
	sel := promptui.Select{
		Label:             tr("Select SSO session to logout (type to filter, Enter to choose)"),
		Items:             choices,
		Searcher:          searcher,
		Templates:         templates,
		StartInSearchMode: true,
		Size:              10,
	}

	idx, _, err := sel.Run()
	if err != nil {
		return "", nil, false, err
	}

	// 处理“全部会话”与单个会话两种情况。
	chosen := choices[idx]
	if chosen.Name == allSessionsLabel {
		return "", nil, true, nil
	}
	return chosen.Name, chosen.Session, false, nil
}

// logoutAllSessions 逐个注销所有已配置的 SSO 会话。
// 若部分会话失败，会汇总错误并返回。
func logoutAllSessions(cfg *Configure) error {
	if cfg == nil {
		return trErrorf("the configuration file cannot be loaded")
	}

	// 对会话名称排序，保证输出顺序稳定。
	sessionNames := make([]string, 0, len(cfg.SsoSession))
	for name := range cfg.SsoSession {
		sessionNames = append(sessionNames, name)
	}
	sort.Strings(sessionNames)

	// 逐个注销，记录失败信息便于集中反馈。
	var failures []string
	for _, name := range sessionNames {
		session := cfg.SsoSession[name]
		if session == nil {
			continue
		}
		sso := &Sso{
			SsoSessionName: name,
			StartURL:       session.StartURL,
			Region:         session.Region,
		}
		if err := sso.Logout(); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(failures) > 0 {
		return trErrorf("failed to logout some sso sessions: %s", strings.Join(failures, "; "))
	}

	return nil
}

// newSsoLogoutCmd 创建 sso logout 子命令：支持指定会话或批量注销。
func newSsoLogoutCmd() *cobra.Command {
	ssoLogoutCmd := &cobra.Command{
		Use:   "logout",
		Short: tr("Perform SSO logout operations"),
		Long:  tr(`Logout from SSO by revoking the cached token and clearing local credentials.`),
		Example: tr(`  # Logout SSO by profile
  volcengine-cli sso logout --profile my-sso-profile
  # Logout SSO by sso-session
  volcengine-cli sso logout --sso-session my-sso-session`),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 读取配置，作为注销目标来源。
			cfg := ctx.config
			if cfg == nil {
				return trErrorf("the configuration file cannot be loaded")
			}

			ssoSessionName := strings.TrimSpace(cmd.Flag("sso-session").Value.String())

			if ssoSessionName != "" {
				// 指定 sso-session 时，直接注销该会话。
				session, ok := cfg.SsoSession[ssoSessionName]
				if !ok {
					return trErrorf("the specified sso-session was not found: %s", ssoSessionName)
				}
				sso := &Sso{
					SsoSessionName: ssoSessionName,
					StartURL:       session.StartURL,
					Region:         session.Region,
				}
				if err := sso.Logout(); err != nil {
					return err
				}
				fmt.Println(tr("logout successfully"))
				return nil
			}

			// 未指定会话时：根据配置情况自动选择或提示用户选择。
			if len(cfg.SsoSession) == 0 {
				return trErrorf("no sso-session configured")
			}
			if len(cfg.SsoSession) == 1 {
				for name, session := range cfg.SsoSession {
					if session == nil {
						return trErrorf("the specified sso-session is invalid: %s", name)
					}
					sso := &Sso{
						SsoSessionName: name,
						StartURL:       session.StartURL,
						Region:         session.Region,
					}
					if err := sso.Logout(); err != nil {
						return err
					}
					fmt.Println(tr("logout successfully"))
					return nil
				}
			}

			// 多个会话时进入交互式选择，支持“全部会话”。
			options := buildSessionOptions(cfg.SsoSession)
			selectedName, selectedSession, logoutAll, err := selectSessionOrAll(options)
			if err != nil {
				return err
			}
			if logoutAll {
				if err := logoutAllSessions(cfg); err != nil {
					return err
				}
				fmt.Println(tr("logout successfully"))
				return nil
			}
			if selectedSession == nil {
				return trErrorf("the specified sso-session is invalid: %s", selectedName)
			}

			// 对单个会话执行注销。
			sso := &Sso{
				SsoSessionName: selectedName,
				StartURL:       selectedSession.StartURL,
				Region:         selectedSession.Region,
			}
			if err := sso.Logout(); err != nil {
				return err
			}
			fmt.Println(tr("logout successfully"))
			return nil
		},
	}

	ssoLogoutCmd.Flags().String("sso-session", "", tr("Specify the SSO session to log out"))

	// 设置自定义Usage模板
	ssoLogoutCmd.SetUsageTemplate(ssoUsageTemplate())

	return ssoLogoutCmd
}

// ssoUsageTemplate 返回 sso 命令族的自定义 Usage 模板。
func ssoUsageTemplate() string {
	return tr("Usage:") + `{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

` + tr("Aliases:") + `
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

` + tr("Examples:") + `
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

` + tr("Available Commands:") + `
{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

` + tr("Flags:") + `
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

` + tr("Global Flags:") + `
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

` + tr("Additional help topics:") + `
{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

` + tr(`Use "{{.CommandPath}} [command] --help" for more information about a command.`) + `{{end}}

` + tr("Fixed Flags:") + `
  ---lang string    ` + tr("Set the display language for this invocation (EN or ZH).") + `
`
}
