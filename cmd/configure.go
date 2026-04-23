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

var configFileDirFunc = util.GetConfigFileDir

// 定义模式枚举常量
const (
	ModeSSO          = "sso"
	ModeAK           = "ak"
	ModeConsoleLogin = "console-login"
	ConfigFile       = "config.json"
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

	if err := json.NewEncoder(tempFile).Encode(config); err != nil {
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

	if profile.AccessKey != "" {
		currentProfile.AccessKey = profile.AccessKey
	}
	if profile.SecretKey != "" {
		currentProfile.SecretKey = profile.SecretKey
	}
	if profile.Region != "" {
		currentProfile.Region = profile.Region
	}
	if profile.Endpoint != "" {
		currentProfile.Endpoint = profile.Endpoint
	}
	if profile.EndpointResolver != "" {
		currentProfile.EndpointResolver = profile.EndpointResolver
	}
	if profile.SessionToken != "" {
		currentProfile.SessionToken = profile.SessionToken
	}
	if profile.DisableSSL != nil {
		*currentProfile.DisableSSL = *profile.DisableSSL
	}
	if profile.UseDualStack != nil {
		if currentProfile.UseDualStack == nil {
			currentProfile.UseDualStack = new(bool)
		}
		*currentProfile.UseDualStack = *profile.UseDualStack
	}
	if profile.SsoSessionName != "" {
		currentProfile.SsoSessionName = profile.SsoSessionName
	}

	cfg.Profiles[currentProfile.Name] = currentProfile
	cfg.Current = currentProfile.Name
	// 写入配置文件，完成持久化。
	return WriteConfigToFile(cfg)
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
