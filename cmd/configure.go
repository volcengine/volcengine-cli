package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/volcengine/volcengine-cli/util"
)

var configFileMu sync.Mutex

// 定义模式枚举常量
const (
	ModeSSO        = "sso"
	ModeAK         = "ak"
	ModeStsToken   = "ststoken"
	ModeRamRoleArn = "ramrolearn"
	ModeOIDC       = "oidc"
	ModeEcsRole    = "ecsrole"

	ConfigFile = "config.json"
)

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
	UseDualStack     *bool  `json:"use-dual-stack,omitempty"`
	SessionToken     string `json:"session-token"`
	DisableSSL       *bool  `json:"disable-ssl"`
	SsoSessionName   string `json:"sso-session-name"`
	AccountId        string `json:"account-id"`
	RoleName         string `json:"role-name"`
	StsExpiration    int64  `json:"sts-expiration"`
	OidcTokenFile    string `json:"oidc-token-file,omitempty"`
	RoleTrn          string `json:"role-trn,omitempty"`
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

	configFileDir, err := util.GetConfigFileDir()
	if err != nil {
		return nil
	}

	if err := os.MkdirAll(configFileDir, 0700); err != nil {
		return nil
	}
	_ = os.Chmod(configFileDir, 0700)

	configFilePath := filepath.Join(configFileDir, ConfigFile)
	file, err := os.OpenFile(configFilePath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		fmt.Println(err)
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

// WriteConfigToFile store config
func WriteConfigToFile(config *Configure) error {
	configFileMu.Lock()
	defer configFileMu.Unlock()

	configFileDir, err := util.GetConfigFileDir()
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

func getConfigProfile(profileName string) error {
	var (
		exist          bool
		currentProfile *Profile
		cfg            *Configure
	)

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		fmt.Println("no profile created")
		return nil
	}

	if profileName == "" {
		fmt.Printf("no profile name specified, show current profile: [%v]\n", cfg.Current)
		profileName = cfg.Current
	}

	// check if the target profile already exists, otherwise print an empty profileFlags
	if currentProfile, exist = cfg.Profiles[profileName]; !exist || currentProfile == nil {
		currentProfile = &Profile{}
	}

	if config == nil || !config.EnableColor {
		util.ShowJson(currentProfile.ToMap(), false)
	} else {
		util.ShowJson(currentProfile.ToMap(), true)
	}
	return nil
}

func listConfigProfiles() error {
	var (
		cfg *Configure
	)

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		fmt.Println("no profile created")
		return nil
	}

	fmt.Printf("*** current profile: %v ***\n", ctx.config.Current)
	for _, profile := range ctx.config.Profiles {
		util.ShowJson(profile.ToMap(), config.EnableColor)
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
		return fmt.Errorf("configuration profile %v not found", profileName)
	}

	// check if the target profileFlags exists
	if _, exist = cfg.Profiles[profileName]; !exist {
		return fmt.Errorf("configuration profile %v not found", profileName)
	}

	// delete profileFlags and write change to config file
	delete(cfg.Profiles, profileName)
	if profileName == cfg.Current {
		cfg.SetRandomCurrentProfile()
		fmt.Printf("delete current profile, set new current profile to [%v]\n", cfg.Current)
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
		return fmt.Errorf("configuration profile %v not found", profileName)
	}

	// check if the target profileFlags exists
	if _, exist = cfg.Profiles[profileName]; !exist {
		return fmt.Errorf("configuration profile %v not found", profileName)
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
