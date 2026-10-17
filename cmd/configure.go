package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/volcengine/volcengine-cli/util"
)

var configFileMu sync.Mutex

var configFileDirFunc = util.GetConfigFileDir

// 定义模式枚举常量
const (
	ModeSSO           = "sso"
	ModeConsoleLogin  = "console-login"
	ModeAK            = "ak"
	ModeRamRoleArn    = "ramrolearn"
	ModeOIDC          = "oidc"
	ModeEcsRole       = "ecsrole"
	ConfigFile        = "config.json"
	maskedConfigValue = "***MASKED***"

	// maskedConfigPadding 是部分遮盖时固定输出的星号个数，不随真实长度变化，避免泄露值的长度。
	maskedConfigPadding = 16
)

// maskRule 描述 `configure get/list` 展示 profile 时某个字段的遮盖方式。
type maskRule struct {
	// Field 是 profile 的 JSON key，例如 "secret-key"。
	Field string
	// KeepLast 为 0 表示完全遮盖；大于 0 表示保留末尾 KeepLast 个字符，其余用星号代替。
	KeepLast int
}

// profileMaskRules 声明默认需要遮盖的 profile 字段。
// 新增敏感字段时只需在此追加一行，并按需指定保留的末尾位数。
var profileMaskRules = []maskRule{
	{Field: "secret-key", KeepLast: 0},
	{Field: "session-token", KeepLast: 0},
	{Field: "access-key", KeepLast: 4},
}

type Configure struct {
	Current     string                 `json:"current"`
	Profiles    map[string]*Profile    `json:"profiles"`
	EnableColor bool                   `json:"enableColor"`
	SsoSession  map[string]*SsoSession `json:"sso-session"`
}

type Profile struct {
	Name             string `json:"name"`
	Mode             string `json:"mode"`
	AccessKey        string `json:"access-key"`
	SecretKey        string `json:"secret-key"`
	Region           string `json:"region"`
	Endpoint         string `json:"endpoint"`
	EndpointResolver string `json:"endpoint-resolver,omitempty"`
	HTTPProxy        string `json:"http-proxy,omitempty"`
	HTTPSProxy       string `json:"https-proxy,omitempty"`
	UseDualStack     *bool  `json:"use-dual-stack,omitempty"`
	SessionToken     string `json:"session-token"`
	DisableSSL       *bool  `json:"disable-ssl"`
	SsoSessionName   string `json:"sso-session-name"`
	AccountId        string `json:"account-id"`
	RoleName         string `json:"role-name"`
	StsExpiration    int64  `json:"sts-expiration"`
	OidcTokenFile    string `json:"oidc-token-file,omitempty"`
	RoleTrn          string `json:"role-trn,omitempty"`
	LoginSession     string `json:"login-session,omitempty"`
}

type SsoSession struct {
	Name               string   `json:"name"`
	StartURL           string   `json:"start-url"`
	Region             string   `json:"region"`
	RegistrationScopes []string `json:"registration-scopes,omitempty"`
}

// LoadConfig from CONFIG_FILE_DIR(default ~/.volcengine)
func LoadConfig() *Configure {
	configFileMu.Lock()
	defer configFileMu.Unlock()

	configFileDir, err := configFileDirFunc()
	if err != nil {
		return nil
	}

	if err := os.MkdirAll(configFileDir, 0700); err != nil {
		return nil
	}
	_ = os.Chmod(configFileDir, 0700)

	// Reading must not create the file: read-only commands such as --help load
	// the config on startup, and an empty config.json fails to parse later.
	configFilePath := filepath.Join(configFileDir, ConfigFile)
	file, err := os.Open(configFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Println(err)
		}
		return nil
	}
	defer file.Close()
	_ = file.Chmod(0600)

	fileContent, err := ioutil.ReadAll(file)
	if err != nil {
		return nil
	}

	cfg := &Configure{}
	err = json.Unmarshal(fileContent, cfg)
	if err != nil {
		return nil
	}

	return cfg
}

// runtimeConfig returns the in-memory config used by the current CLI process.
// Prefer this over reloading from disk so command handlers operate on a single
// config object during one invocation.
func runtimeConfig() *Configure {
	if ctx != nil && ctx.config != nil {
		return ctx.config
	}
	return config
}

// setRuntimeConfig keeps the global config references in sync after updates.
func setRuntimeConfig(cfg *Configure) {
	if ctx != nil {
		ctx.config = cfg
	}
	config = cfg
}

// setEnableColor persists the color preference, initializing the config when
// no config file exists yet (e.g. a fresh install).
func setEnableColor(enabled bool) error {
	cfg := runtimeConfig()
	if cfg == nil {
		cfg = &Configure{Profiles: make(map[string]*Profile)}
	}
	cfg.EnableColor = enabled
	if err := WriteConfigToFile(cfg); err != nil {
		return err
	}
	setRuntimeConfig(cfg)
	return nil
}

// WriteConfigToFile store config
func WriteConfigToFile(config *Configure) error {
	configFileMu.Lock()
	defer configFileMu.Unlock()

	configFileDir, err := configFileDirFunc()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(configFileDir, 0700); err != nil {
		return err
	}
	_ = os.Chmod(configFileDir, 0700)

	targetPath := filepath.Join(configFileDir, ConfigFile)

	dir := filepath.Dir(targetPath)
	tempFile, err := os.CreateTemp(dir, ".tmp-config-*")
	if err != nil {
		return err
	}
	tempName := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempName)
	}()
	_ = tempFile.Chmod(0600)

	data, err := marshalConfig(config)
	if err != nil {
		return err
	}
	if _, err := tempFile.Write(data); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	if err := os.Rename(tempName, targetPath); err != nil {
		_ = os.Remove(targetPath)
		if err2 := os.Rename(tempName, targetPath); err2 != nil {
			return err2
		}
	}
	_ = os.Chmod(targetPath, 0600)
	return nil
}

func marshalConfig(config *Configure) ([]byte, error) {
	data, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (config *Configure) SetRandomCurrentProfile() {
	if config == nil {
		return
	}

	if config.Profiles == nil || len(config.Profiles) == 0 {
		config.Current = ""
		return
	}

	config.Current = ""
	for key := range config.Profiles {
		if config.Current == "" {
			config.Current = key
			break
		}
	}
}

func setConfigProfile(profile *Profile) error {
	var (
		exist          bool
		currentProfile *Profile
		cfg            *Configure
	)

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		cfg = &Configure{
			Profiles: make(map[string]*Profile),
		}
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]*Profile)
	}

	// check if the target profileFlags already exists
	// otherwise create a new profileFlags
	if currentProfile, exist = cfg.Profiles[profile.Name]; !exist {
		currentProfile = &Profile{
			Name:         profile.Name,
			Mode:         ModeAK,
			DisableSSL:   new(bool),
			UseDualStack: new(bool),
		}
		*currentProfile.DisableSSL = false
		*currentProfile.UseDualStack = false
	}

	nextProfile := mergeProfile(currentProfile, profile)
	if err := validateProfileMode(nextProfile); err != nil {
		return err
	}

	cfg.Profiles[nextProfile.Name] = nextProfile
	cfg.Current = nextProfile.Name
	// 写入配置文件，完成持久化。
	return WriteConfigToFile(cfg)
}

func mergeProfile(base *Profile, input *Profile) *Profile {
	merged := cloneProfile(base)
	if merged == nil {
		merged = &Profile{}
	}

	if input == nil {
		return merged
	}

	if input.Name != "" {
		merged.Name = input.Name
	}
	if input.AccessKey != "" {
		merged.AccessKey = input.AccessKey
	}
	if input.SecretKey != "" {
		merged.SecretKey = input.SecretKey
	}
	if input.Region != "" {
		merged.Region = input.Region
	}
	if input.Endpoint != "" {
		merged.Endpoint = input.Endpoint
	}
	if input.EndpointResolver != "" {
		merged.EndpointResolver = input.EndpointResolver
	}
	if input.HTTPProxy != "" {
		merged.HTTPProxy = input.HTTPProxy
	}
	if input.HTTPSProxy != "" {
		merged.HTTPSProxy = input.HTTPSProxy
	}
	if input.SessionToken != "" {
		merged.SessionToken = input.SessionToken
	}
	if input.DisableSSL != nil {
		if merged.DisableSSL == nil {
			merged.DisableSSL = new(bool)
		}
		*merged.DisableSSL = *input.DisableSSL
	}
	if input.UseDualStack != nil {
		if merged.UseDualStack == nil {
			merged.UseDualStack = new(bool)
		}
		*merged.UseDualStack = *input.UseDualStack
	}
	if input.SsoSessionName != "" {
		merged.SsoSessionName = input.SsoSessionName
	}
	if input.AccountId != "" {
		merged.AccountId = input.AccountId
	}
	if input.RoleName != "" {
		merged.RoleName = input.RoleName
	}
	if input.OidcTokenFile != "" {
		merged.OidcTokenFile = input.OidcTokenFile
	}
	if input.RoleTrn != "" {
		merged.RoleTrn = input.RoleTrn
	}
	if input.Mode != "" {
		merged.Mode = input.Mode
	}
	// 仅新建 profile 时默认 mode 为 ak，修改已有 profile 时保留原 mode
	if base == nil && merged.Mode == "" {
		merged.Mode = ModeAK
	}

	return merged
}

func cloneProfile(p *Profile) *Profile {
	if p == nil {
		return nil
	}

	clone := *p
	if p.DisableSSL != nil {
		clone.DisableSSL = new(bool)
		*clone.DisableSSL = *p.DisableSSL
	}
	if p.UseDualStack != nil {
		clone.UseDualStack = new(bool)
		*clone.UseDualStack = *p.UseDualStack
	}
	return &clone
}

func getConfigProfile(profileName string, showSensitive bool) error {
	var (
		exist          bool
		currentProfile *Profile
		cfg            *Configure
	)

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		fmt.Println(tr("no profile created"))
		return nil
	}

	if profileName == "" {
		fmt.Printf(tr("no profile name specified, show current profile: [%v]\n"), cfg.Current)
		profileName = cfg.Current
	}

	// check if the target profile already exists, otherwise print an empty profileFlags
	if currentProfile, exist = cfg.Profiles[profileName]; !exist || currentProfile == nil {
		currentProfile = &Profile{}
	}

	util.ShowJson(currentProfile.ToDisplayMap(showSensitive), cfg.EnableColor)
	return nil
}

func listConfigProfiles(showSensitive bool) error {
	var (
		cfg *Configure
	)

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		fmt.Println(tr("no profile created"))
		return nil
	}

	fmt.Printf(tr("*** current profile: %v ***\n"), ctx.config.Current)
	for _, profile := range ctx.config.Profiles {
		util.ShowJson(profile.ToDisplayMap(showSensitive), cfg.EnableColor)
	}
	return nil
}

func deleteConfigProfile(profileName string) error {
	var (
		exist bool
		cfg   *Configure
	)

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		return trErrorf("configuration profile %v not found", profileName)
	}

	// check if the target profileFlags exists
	if _, exist = cfg.Profiles[profileName]; !exist {
		return trErrorf("configuration profile %v not found", profileName)
	}

	// delete profileFlags and write change to config file
	delete(cfg.Profiles, profileName)
	if profileName == cfg.Current {
		cfg.SetRandomCurrentProfile()
		fmt.Printf(tr("delete current profile, set new current profile to [%v]\n"), cfg.Current)
	}

	// 写入配置文件，完成持久化。
	return WriteConfigToFile(cfg)
}

func changeConfigProfile(profileName string) error {
	var (
		exist bool
		cfg   *Configure
	)

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		return trErrorf("configuration profile %v not found", profileName)
	}

	// check if the target profileFlags exists
	if _, exist = cfg.Profiles[profileName]; !exist {
		return trErrorf("configuration profile %v not found", profileName)
	}

	// if not change,skip it
	if profileName == cfg.Current {
		return nil
	}

	// change current
	cfg.Current = profileName
	// 写入配置文件，完成持久化。
	return WriteConfigToFile(cfg)
}

func (p *Profile) ToMap() map[string]interface{} {
	data, _ := json.Marshal(p)
	m := make(map[string]interface{})
	json.Unmarshal(data, &m)

	return m
}

// ToDisplayMap 返回用于展示的 profile map。
// showSensitive 为 false 时，按 profileMaskRules 遮盖敏感字段；该方法只改动返回的副本，不影响 profile 本身。
func (p *Profile) ToDisplayMap(showSensitive bool) map[string]interface{} {
	m := p.ToMap()
	if showSensitive {
		return m
	}

	for _, rule := range profileMaskRules {
		if value, ok := m[rule.Field].(string); ok && value != "" {
			m[rule.Field] = maskConfigValue(value, rule.KeepLast)
		}
	}
	return m
}

// maskConfigValue 按 keepLast 遮盖 value。
//   - keepLast <= 0：完全遮盖，返回 maskedConfigValue。
//   - keepLast > 0：返回固定个数的星号加末尾 keepLast 个字符。
//
// 当值的长度不超过 2*keepLast 时，保留末尾字符会暴露过半内容，此时退回完全遮盖。
// 按 rune 截取，避免切断多字节字符。空字符串原样返回。
func maskConfigValue(value string, keepLast int) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if keepLast <= 0 || len(runes) <= keepLast*2 {
		return maskedConfigValue
	}
	return strings.Repeat("*", maskedConfigPadding) + string(runes[len(runes)-keepLast:])
}

func (p *Profile) String() string {
	b, _ := json.MarshalIndent(p, "", "    ")
	return string(b)
}

// setSsoSession 保存/更新 SSO 会话配置。
// 该函数会规范化 scopes，初始化配置结构，并将会话写入配置文件。
func setSsoSession(session *SsoSession) error {
	var (
		cfg *Configure
	)
	scopes, err := normalizeRegistrationScopes(session.RegistrationScopes)
	if err != nil {
		return err
	}

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		cfg = &Configure{
			Profiles:   make(map[string]*Profile),
			SsoSession: make(map[string]*SsoSession),
		}
	}

	// 确保 SsoSession 映射已初始化。
	if cfg.SsoSession == nil {
		cfg.SsoSession = make(map[string]*SsoSession)
	}

	// 构建新会话对象，使用规范化后的 scopes。
	newSession := &SsoSession{
		Name:               session.Name,
		StartURL:           session.StartURL,
		Region:             session.Region,
		RegistrationScopes: scopes,
	}

	// 写入内存配置并提示成功。
	cfg.SsoSession[session.Name] = newSession

	// 写入配置文件，完成持久化。
	return WriteConfigToFile(cfg)
}
