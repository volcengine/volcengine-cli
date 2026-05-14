package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/template"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var (
	profileFlags    Profile
	ssoSessionFlags SsoSession
	ssoFlags        Profile
)

const defaultSsoRegion = "cn-beijing"

var defaultRegistrationScopes = []string{"cloudidentity:account:access", "offline_access"}
var allowedRegistrationScopes = []string{"cloudidentity:account:access", "offline_access"}
var allowedRegistrationScopesSet = map[string]struct{}{
	"cloudidentity:account:access": {},
	"offline_access":               {},
}

func init() {
	configureCmd := newConfigureRootCmd()

	configureCmd.AddCommand(newConfigureGetCmd())
	configureCmd.AddCommand(newConfigureListCmd())
	configureCmd.AddCommand(newConfigureDeleteCmd())
	configureCmd.AddCommand(newConfigureProfileCmd())
	configureCmd.AddCommand(newConfigureSetCmd())
	configureCmd.AddCommand(newConfigureSsoSessionCmd())
	configureCmd.AddCommand(newConfigureSsoCmd())

	rootCmd.AddCommand(configureCmd)
}

func newConfigureRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "configure",
		Args: cobra.MatchAll(cobra.OnlyValidArgs),
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Usage()
		},
	}

	cmd.SetUsageTemplate(configureUsageTemplate())

	return cmd
}

func newConfigureGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "get",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := cmd.Flag("profile").Value.String()
			return getConfigProfile(profileName)
		},
		Short: "show target profile's information",
		Long: `Description:
  show target profile's information
  if no profile name specified, show default profile`,
		DisableFlagsInUseLine: true,
	}

	cmd.SetUsageTemplate(configureActionUsageTemplate())

	cmd.Flags().StringVar(&profileFlags.Name, "profile", "", "target profile name")
	cmd.Flags().BoolP("help", "h", false, "")

	return cmd
}

func newConfigureSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "set",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setConfigProfile(&profileFlags)
		},
		Short: "add new profile, or modify target profile",
		Long: `Description:
  add new profile, or modify target profile:
      1. if profile not exist, add new;
      2. if profile exist, modify target field

  supported modes: ak, ststoken, sso, ramrolearn, oidc, ecsrole`,
		DisableFlagsInUseLine: true,
	}

	cmd.SetUsageTemplate(configureActionUsageTemplate())

	cmd.Flags().StringVar(&profileFlags.Name, "profile", "", "target profile name")
	cmd.Flags().StringVar(&profileFlags.Mode, "mode", "", "credential mode (ak, ststoken, sso, ramrolearn, oidc, ecsrole)")
	cmd.Flags().StringVar(&profileFlags.AccessKey, "access-key", "", "your access key(AK)")
	cmd.Flags().StringVar(&profileFlags.SecretKey, "secret-key", "", "your secret key(SK)")
	cmd.Flags().StringVar(&profileFlags.Region, "region", "", "your region")
	cmd.Flags().StringVar(&profileFlags.Endpoint, "endpoint", "", "endpoint bind with region")
	cmd.Flags().StringVar(&profileFlags.EndpointResolver, "endpoint-resolver", "", "endpoint resolver (auto-addressing)")
	cmd.Flags().StringVar(&profileFlags.SessionToken, "session-token", "", "your session token")
	cmd.Flags().StringVar(&profileFlags.SsoSessionName, "sso-session", "", "your sso session name")
	cmd.Flags().StringVar(&profileFlags.AccountId, "account-id", "", "your account id (required for ramrolearn mode)")
	cmd.Flags().StringVar(&profileFlags.RoleName, "role-name", "", "your role name (required for ramrolearn/ecsrole mode)")
	cmd.Flags().StringVar(&profileFlags.OidcTokenFile, "oidc-token-file", "", "path to OIDC token file (required for oidc mode)")
	cmd.Flags().StringVar(&profileFlags.RoleTrn, "role-trn", "", "role TRN (required for oidc mode)")

	profileFlags.DisableSSL = cmd.Flags().Bool("disable-ssl", false, "disable ssl")
	profileFlags.UseDualStack = cmd.Flags().Bool("use-dual-stack", false, "use dual-stack endpoints")
	cmd.Flags().BoolP("help", "h", false, "")

	cmd.MarkFlagRequired("profile")

	return cmd
}

// validateProfileMode 校验 profile 的 mode 及其必填参数
func validateProfileMode(profile *Profile) error {
	mode := strings.ToLower(strings.TrimSpace(profile.Mode))
	switch mode {
	case "", ModeAK:
		if profile.AccessKey == "" {
			return fmt.Errorf("mode %q requires --access-key", ModeAK)
		}
		if profile.SecretKey == "" {
			return fmt.Errorf("mode %q requires --secret-key", ModeAK)
		}
	case ModeSSO:
		// sso 模式通过 configure sso 子命令配置，此处不额外校验
	case ModeRamRoleArn:
		if profile.AccessKey == "" {
			return fmt.Errorf("mode %q requires --access-key", ModeRamRoleArn)
		}
		if profile.SecretKey == "" {
			return fmt.Errorf("mode %q requires --secret-key", ModeRamRoleArn)
		}
		if profile.RoleName == "" {
			return fmt.Errorf("mode %q requires --role-name", ModeRamRoleArn)
		}
		if profile.AccountId == "" {
			return fmt.Errorf("mode %q requires --account-id", ModeRamRoleArn)
		}
	case ModeOIDC:
		if profile.OidcTokenFile == "" {
			return fmt.Errorf("mode %q requires --oidc-token-file", ModeOIDC)
		}
		if profile.RoleTrn == "" {
			return fmt.Errorf("mode %q requires --role-trn", ModeOIDC)
		}
	case ModeEcsRole:
		if profile.RoleName == "" {
			return fmt.Errorf("mode %q requires --role-name", ModeEcsRole)
		}
	default:
		return fmt.Errorf("unsupported mode %q, supported modes: ak, ststoken, sso, ramrolearn, oidc, ecsrole", mode)
	}
	return nil
}

func newConfigureListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listConfigProfiles()
		},
		Short: "list all profiles",
		Long: `Description:
  list all profiles`,
		DisableFlagsInUseLine: true,
	}

	cmd.SetUsageTemplate(configureActionUsageTemplate())

	cmd.Flags().BoolP("help", "h", false, "")

	return cmd
}

func newConfigureDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "delete",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := cmd.Flag("profile").Value.String()
			return deleteConfigProfile(profileName)
		},
		Short: "delete target profile",
		Long: `Description:
  delete target profile`,
		DisableFlagsInUseLine: true,
	}

	cmd.SetUsageTemplate(configureActionUsageTemplate())

	cmd.Flags().StringVar(&profileFlags.Name, "profile", "", "target profile name")
	cmd.Flags().BoolP("help", "h", false, "")

	cmd.MarkFlagRequired("profile")

	return cmd
}

func newConfigureProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := cmd.Flag("profile").Value.String()
			return changeConfigProfile(profileName)
		},
		Short: "change target profile",
		Long: `Description:
  change target profile`,
		DisableFlagsInUseLine: true,
	}

	cmd.SetUsageTemplate(configureActionUsageTemplate())

	cmd.Flags().StringVar(&profileFlags.Name, "profile", "", "target profile name")
	cmd.Flags().BoolP("help", "h", false, "")

	cmd.MarkFlagRequired("profile")

	return cmd
}

// newConfigureSsoSessionCmd 构建 `configure sso-session` 子命令。
// 该命令负责新增或更新 SSO 会话：支持交互式输入、基于已有会话的默认值回填，并统一做参数校验与规范化。
func newConfigureSsoSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "sso-session",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 初始化配置对象与会话映射，保证后续读写安全。
			cfg := ctx.config
			if cfg == nil {
				cfg = &Configure{
					Profiles:   make(map[string]*Profile),
					SsoSession: make(map[string]*SsoSession),
				}
				ctx.config = cfg
			}
			if cfg.SsoSession == nil {
				cfg.SsoSession = make(map[string]*SsoSession)
			}

			// 确定要操作的会话名称；若未传参则进入交互式选择/创建流程。
			var existingSession *SsoSession
			if strings.TrimSpace(ssoSessionFlags.Name) == "" {
				name, selected, err := promptSessionName(cfg, "")
				if err != nil {
					return err
				}
				ssoSessionFlags.Name = name
				existingSession = selected
			} else {
				ssoSessionFlags.Name = strings.TrimSpace(ssoSessionFlags.Name)
				existingSession = cfg.SsoSession[ssoSessionFlags.Name]
			}

			// 以已有会话作为默认值，降低重复输入成本。
			defaultStartURL := ""
			defaultRegion := defaultSsoRegion
			defaultScopes := []string(nil)
			if existingSession != nil {
				defaultStartURL = existingSession.StartURL
				defaultRegion = existingSession.Region
				defaultScopes = existingSession.RegistrationScopes
			}

			// 依次采集必须字段：StartURL 与 Region 支持默认值回填。
			if err := promptForRequiredStringWithDefault(&ssoSessionFlags.StartURL, "Please enter SSO Start URL:", "SSO Start URL", defaultStartURL); err != nil {
				return err
			}
			if err := promptForRequiredStringWithDefault(&ssoSessionFlags.Region, "Please enter SSO region:", "SSO region", defaultRegion); err != nil {
				return err
			}
			// 采集并规范化 scopes：支持参数输入或交互式输入，并去重校验。
			var scopes []string
			var err error
			if len(ssoSessionFlags.RegistrationScopes) == 0 {
				showDefault := existingSession == nil
				scopes, err = promptForRegistrationScopesWithDefault(defaultScopes, showDefault)
			} else {
				scopes, err = normalizeRegistrationScopes(ssoSessionFlags.RegistrationScopes)
			}
			if err != nil {
				return err
			}
			ssoSessionFlags.RegistrationScopes = scopes
			// 将 SSO 会话落盘到配置文件。
			err = setSsoSession(&ssoSessionFlags)
			if err != nil {
				return err
			}
			fmt.Printf("SSO session [%s] configured successfully.\n", ssoSessionFlags.Name)
			return nil
		},
		Short: "add or modify SSO session",
		Long: `Description:
  add new SSO session, or modify target SSO session:
      1. if SSO session not exist, add new;
      2. if SSO session exist, modify target field`,
		DisableFlagsInUseLine: true,
	}

	cmd.SetUsageTemplate(configureActionUsageTemplate())

	// 同时支持参数式输入，便于脚本化配置。
	cmd.Flags().StringVar(&ssoSessionFlags.Name, "name", "", "SSO session name")
	cmd.Flags().StringVar(&ssoSessionFlags.StartURL, "start-url", "", "SSO start URL")
	cmd.Flags().StringVar(&ssoSessionFlags.Region, "region", "", "SSO region")
	cmd.Flags().StringSliceVar(&ssoSessionFlags.RegistrationScopes, "registration-scopes", nil, "comma-separated SSO registration scopes (cloudidentity:account:access,offline_access)")
	cmd.Flags().BoolP("help", "h", false, "")

	return cmd
}

// promptForRequiredStringWithDefault 读取必填字符串；当已有默认值时支持回车沿用。
// 该函数会循环提示直到得到非空值，避免后续逻辑处理空字段。
func promptForRequiredStringWithDefault(target *string, prompt, fieldName, defaultValue string) error {
	for {
		if target == nil || strings.TrimSpace(*target) == "" {
			if strings.TrimSpace(defaultValue) != "" {
				// 有默认值时提示并允许直接回车使用默认值。
				fmt.Printf("%s [%s]:", prompt, defaultValue)
				line, err := readLineAllowEmpty()
				if err != nil {
					return err
				}
				line = strings.TrimSpace(line)
				if line == "" {
					*target = defaultValue
				} else {
					*target = line
				}
			} else {
				// 无默认值时必须输入。
				fmt.Printf("%s", prompt)
				line, err := readLineAllowEmpty()
				if err != nil {
					return err
				}
				*target = strings.TrimSpace(line)
			}
		}
		*target = strings.TrimSpace(*target)
		if *target != "" {
			return nil
		}
		// 兜底提示：空值会被拒绝并重新输入。
		fmt.Printf("%s cannot be empty\n", fieldName)
		*target = ""
	}
}

// promptForRegistrationScopes 交互式读取 registration scopes，并做统一规范化处理。
// 当未提供任何值时会提示用户输入，最终返回去重且校验通过的 scope 列表。
func promptForRegistrationScopes(current []string) ([]string, error) {
	if len(current) == 0 {
		fmt.Printf("Please enter SSO registration scopes (comma-separated, allowed: %s) [%s]:", strings.Join(allowedRegistrationScopes, ", "), strings.Join(defaultRegistrationScopes, ","))
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			current = strings.Split(line, ",")
		}
	}
	return normalizeRegistrationScopes(current)
}

// promptForRegistrationScopesWithDefault 支持带默认值的 scopes 输入。
// showDefault 为 true 时会展示默认值标签，否则仅在已有值时展示。
func promptForRegistrationScopesWithDefault(current []string, showDefault bool) ([]string, error) {
	defaultValue := strings.Join(current, ",")
	label := ""
	if showDefault {
		if defaultValue == "" {
			defaultValue = strings.Join(defaultRegistrationScopes, ",")
		}
		label = fmt.Sprintf("[%s]", defaultValue)
	} else if defaultValue != "" {
		label = fmt.Sprintf("[%s]", defaultValue)
	}
	fmt.Printf("Please enter SSO registration scopes (comma-separated, allowed: %s) %s:", strings.Join(allowedRegistrationScopes, ", "), label)
	line, err := readLineAllowEmpty()
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line != "" {
		current = strings.Split(line, ",")
	}
	return normalizeRegistrationScopes(current)
}

// normalizeRegistrationScopes 对输入的 scopes 做清洗、校验与去重。
// - 空输入：返回默认 scopes
// - 非法值：返回错误
// - 重复值：去重保留首次出现的顺序
func normalizeRegistrationScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return append([]string(nil), defaultRegistrationScopes...), nil
	}
	seen := make(map[string]struct{})
	var normalized []string
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := allowedRegistrationScopesSet[scope]; !ok {
			return nil, fmt.Errorf("invalid SSO registration scope %q, allowed values: %s", scope, strings.Join(allowedRegistrationScopes, ", "))
		}
		if _, exists := seen[scope]; !exists {
			seen[scope] = struct{}{}
			normalized = append(normalized, scope)
		}
	}
	if len(normalized) == 0 {
		return append([]string(nil), defaultRegistrationScopes...), nil
	}
	return normalized, nil
}

// newConfigureSsoCmd 构建 `configure sso` 子命令。
// 该命令会关联 SSO 会话，执行 SSO 授权流程并最终写入 SSO 类型的 profile 配置。
func newConfigureSsoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "sso",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载并初始化配置，保证 profiles 与 sso-session 映射存在。
			cfg := ctx.config
			if cfg == nil {
				cfg = &Configure{
					Profiles:   make(map[string]*Profile),
					SsoSession: make(map[string]*SsoSession),
				}
				ctx.config = cfg
			}
			if cfg.Profiles == nil {
				cfg.Profiles = make(map[string]*Profile)
			}
			if cfg.SsoSession == nil {
				cfg.SsoSession = make(map[string]*SsoSession)
			}

			// 读取 CLI 标志位，控制设备码流程与浏览器自动打开行为。
			noBrowser, err := cmd.Flags().GetBool("no-browser")
			if err != nil {
				return err
			}

			// 读取 profile 名称：未输入时允许回车留空，稍后由 SSO 信息回填默认值。
			if strings.TrimSpace(ssoFlags.Name) == "" {
				fmt.Print("Enter profile name (press Enter to use default: {sso-role-name}-{sso-account-id}): ")
				line, err := readLineAllowEmpty()
				if err != nil {
					return err
				}
				ssoFlags.Name = line
			}

			profile := &Profile{
				Name: ssoFlags.Name,
			}

			if inputProfile := cfg.Profiles[ssoFlags.Name]; inputProfile != nil {
				profile = inputProfile
			}

			// Prompt for SSO session name with live fuzzy filtering and allow creating new.
			var (
				name            string
				existingSession *SsoSession
			)
			if ssoFlags.SsoSessionName == "" {
				// 交互式选择或创建会话；会话名不可重复。
				for {
					name, existingSession, err = promptSessionName(cfg, ssoFlags.SsoSessionName)
					if err == nil {
						break
					}
					if errors.Is(err, errSessionExists) {
						fmt.Println(err.Error())
						continue
					}
					return err
				}
				ssoFlags.SsoSessionName = name
			} else {
				existingSession = cfg.SsoSession[ssoFlags.SsoSessionName]
			}

			// 若会话不存在则引导创建，并写入配置文件。
			ssoSession := existingSession
			if ssoSession == nil {
				ssoSession, err = createSsoSessionInSso(ssoFlags.SsoSessionName, cfg)
				if err != nil {
					return err
				}
			}

			// 构建 SSO 服务实例，组装所需的会话与运行参数。
			var sso SSOService = &Sso{
				Profile:        profile,
				SsoSessionName: ssoFlags.SsoSessionName,
				StartURL:       ssoSession.StartURL,
				Region:         ssoSession.Region,
				Scopes:         ssoSession.RegistrationScopes,
				UseDeviceCode:  true, // 目前仅支持设备码登录流程。
				NoBrowser:      noBrowser,
			}

			// 执行 SSO 授权流程并落盘 profile 配置。
			err = sso.SetProfile()
			if err != nil {
				return err
			}
			fmt.Printf("SSO profile [%s] configured successfully.\n", profile.Name)
			return nil
		},
		Short: "configure SSO type profile",
		Long: `Description:
  configure SSO type profile with profile.mode=sso
  this command will guide you through the SSO authorization process
  and save the profile configuration to ~/.volcengine/config.json
  if the specified SSO session doesn't exist, it will be created automatically`,
		DisableFlagsInUseLine: true,
	}

	cmd.SetUsageTemplate(configureActionUsageTemplate())

	cmd.Flags().StringVar(&ssoFlags.Name, "profile", "", "profile name")
	cmd.Flags().StringVar(&ssoFlags.SsoSessionName, "sso-session", "", "SSO session name")
	cmd.Flags().Bool("no-browser", false, "Do not automatically open the browser during device authorization")
	cmd.Flags().BoolP("help", "h", false, "")

	return cmd
}

// readLineAllowEmpty 从标准输入读取一行，允许空输入并去除首尾空白。
func readLineAllowEmpty() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

type sessionOption struct {
	Name    string
	Session *SsoSession
}

var errSessionExists = errors.New("SSO session already exists")

// promptSessionName 获取 SSO 会话名称：
// - 若配置中无会话，直接提示输入并校验非空；
// - 若已有会话，进入交互式选择/创建流程。
func promptSessionName(cfg *Configure, defaultName string) (string, *SsoSession, error) {
	if cfg == nil || len(cfg.SsoSession) == 0 {
		// 没有任何已存在的会话时，直接使用简单输入流程。
		fmt.Print("Please enter SSO session name:")
		name, err := readLineAllowEmpty()
		if err != nil {
			return "", nil, err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return "", nil, fmt.Errorf("SSO session name cannot be empty")
		}
		return name, nil, nil
	}

	options := buildSessionOptions(cfg.SsoSession)
	selectedName, selectedSession, err := runSessionSelect(cfg, options, defaultName)
	if err != nil {
		return "", nil, err
	}

	return selectedName, selectedSession, nil
}

// buildSessionOptions 将会话映射转换为稳定排序的选项列表，便于交互式展示。
func buildSessionOptions(all map[string]*SsoSession) []sessionOption {
	var keys []string
	for name := range all {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var out []sessionOption
	for _, name := range keys {
		out = append(out, sessionOption{
			Name:    name,
			Session: all[name],
		})
	}
	return out
}

const addNewSessionLabel = "<Create new session>"

// runSessionSelect 使用 promptui 提供交互式会话选择：
// - 支持搜索过滤；
// - 可选择“创建新会话”；
// - 返回最终选中的会话名称与对象（新建时对象为 nil）。
func runSessionSelect(cfg *Configure, options []sessionOption, defaultName string) (string, *SsoSession, error) {
	choices := make([]sessionOption, 0, len(options)+1)
	choices = append(choices, options...)
	choices = append(choices, sessionOption{Name: addNewSessionLabel, Session: nil})

	var lastSearchInput string
	searcher := func(input string, index int) bool {
		if index < 0 || index >= len(choices) {
			return false
		}
		rawInput := strings.TrimSpace(input)
		lastSearchInput = rawInput
		lowerInput := strings.ToLower(rawInput)
		item := choices[index]
		// 始终展示“创建新会话”选项，方便直接新增。
		if item.Name == addNewSessionLabel {
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

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "{{if isNew .}}▸ {{ .Name | green }}{{else}}▸ {{ .Name | cyan }}   {{ sessionRegion .Session }}   {{ sessionStart .Session }}{{end}}",
		Inactive: "{{if isNew .}}  {{ .Name | faint }}{{else}}  {{ .Name | faint }}   {{ sessionRegion .Session }}   {{ sessionStart .Session }}{{end}}",
		Selected: "{{if isNew .}}✔ {{ .Name }}{{else}}✔ {{ .Name }}{{end}}",
		Details: `
--------- SSO Session ----------
Name:   {{ .Name }}
Region: {{ sessionRegion .Session }}
URL:    {{ sessionStart .Session }}
Scopes: {{ sessionScopes .Session }}`,
		FuncMap: buildPromptFuncMap(),
	}

	sel := promptui.Select{
		Label:             "Select or create SSO session (type to filter, Enter to choose)",
		Items:             choices,
		Searcher:          searcher,
		Templates:         templates,
		StartInSearchMode: true,
		Size:              10,
	}

	idx, _, err := sel.Run()
	if err != nil {
		return "", nil, err
	}

	chosen := choices[idx]
	if chosen.Name == addNewSessionLabel {
		// 新建会话名称：优先使用默认值，其次使用最近一次的搜索输入。
		defaultNewName := strings.TrimSpace(defaultName)
		if defaultNewName == "" {
			defaultNewName = lastSearchInput
		}
		newNamePrompt := promptui.Prompt{
			Label:     "Enter new SSO session name",
			Default:   defaultNewName,
			AllowEdit: true,
			Validate: func(input string) error {
				if strings.TrimSpace(input) == "" {
					return fmt.Errorf("SSO session name cannot be empty")
				}
				if _, ok := cfg.SsoSession[input]; ok {
					return fmt.Errorf("%w: %s", errSessionExists, input)
				}
				return nil
			},
		}
		newName, err := newNamePrompt.Run()
		if err != nil {
			return "", nil, err
		}
		return strings.TrimSpace(newName), nil, nil
	}

	return chosen.Name, chosen.Session, nil
}

// buildPromptFuncMap 扩展 promptui 模板函数：
// 用于渲染会话列表中的“是否新建/区域/URL/Scopes”等字段。
func buildPromptFuncMap() template.FuncMap {
	fm := template.FuncMap{}
	for k, v := range promptui.FuncMap {
		fm[k] = v
	}
	fm["isNew"] = func(opt sessionOption) bool {
		return opt.Session == nil && opt.Name == addNewSessionLabel
	}
	fm["sessionRegion"] = func(s *SsoSession) string {
		if s == nil {
			return ""
		}
		return s.Region
	}
	fm["sessionStart"] = func(s *SsoSession) string {
		if s == nil {
			return ""
		}
		return s.StartURL
	}
	fm["sessionScopes"] = func(s *SsoSession) string {
		if s == nil || len(s.RegistrationScopes) == 0 {
			return "-"
		}
		return strings.Join(s.RegistrationScopes, ",")
	}
	return fm
}

// createSsoSessionInSso 在 SSO 会话不存在时创建新会话并写入配置文件。
// 该流程采用交互式输入，完成 StartURL、Region 与 Scopes 的采集与校验。
func createSsoSessionInSso(sessionName string, cfg *Configure) (*SsoSession, error) {
	newSession := &SsoSession{
		Name: sessionName,
	}

	// 读取 StartURL，空值直接报错。
	fmt.Print("Please enter SSO start URL:")
	fmt.Scanln(&newSession.StartURL)
	if newSession.StartURL == "" {
		return nil, fmt.Errorf("SSO start URL cannot be empty")
	}

	// 读取 Region，空值直接报错。
	fmt.Print("Please enter SSO region [cn-beijing]:")
	fmt.Scanln(&newSession.Region)
	if newSession.Region == "" {
		newSession.Region = defaultSsoRegion
	}
	// 读取并规范化 scopes，保证值合法且无重复。
	scopes, err := promptForRegistrationScopes(newSession.RegistrationScopes)
	if err != nil {
		return nil, err
	}
	newSession.RegistrationScopes = scopes

	// 将新会话保存到内存配置。
	cfg.SsoSession[sessionName] = newSession

	// 写入配置文件，确保会话持久化。
	err = WriteConfigToFile(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to save SSO session configuration: %v", err)
	}

	return newSession, nil
}

func configureUsageTemplate() string {
	return `Usage:{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
}

func configureActionUsageTemplate() string {
	return `Usage:{{if .Runnable}}
  {{.UseLine}} [params]{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
}
