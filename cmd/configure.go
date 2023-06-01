package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"encoding/json"
	"fmt"
	"github.com/volcengine/volcengine-cli/util"
	"github.com/volcengine/volcengine-go-sdk/volcengine/volcengineutil"
	"io/ioutil"
	"os"
)

const ConfigFile = "config.json"

type Configure struct {
	Current     string              `json:"current"`
	Profiles    map[string]*Profile `json:"profiles"`
	EnableColor bool                `json:"enableColor"`
}

type Profile struct {
	Name         string `json:"name"`
	Mode         string `json:"mode"`
	AccessKey    string `json:"access-key"`
	SecretKey    string `json:"secret-key"`
	Region       string `json:"region"`
	Endpoint     string `json:"endpoint"`
	SessionToken string `json:"session-token"`
	DisableSSL   *bool  `json:"disable-ssl"`
}

// LoadConfig from CONFIG_FILE_DIR(default ~/.volcengine)
func LoadConfig() *Configure {
	configFileDir, err := util.GetConfigFileDir()
	if err != nil {
		return nil
	}

	if _, err = os.Stat(configFileDir); err != nil {
		if os.IsNotExist(err) {
			os.MkdirAll(configFileDir, 0755)
		}
	}

	if _, err = os.Stat(configFileDir + ConfigFile); err != nil {
		if os.IsNotExist(err) {
			// todo handle err
		}
	}

	file, err := os.OpenFile(configFileDir+ConfigFile, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	defer file.Close()

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
	configFileDir, err := util.GetConfigFileDir()
	if err != nil {
		return nil
	}

	file, err := os.OpenFile(configFileDir+ConfigFile, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.Encode(config)
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

	// if config not exist, create an empty config
	if cfg = ctx.config; cfg == nil {
		cfg = &Configure{
			Profiles: make(map[string]*Profile),
		}
	}

	// check if the target profileFlags already exists
	// otherwise create a new profileFlags
	if currentProfile, exist = cfg.Profiles[profile.Name]; !exist {
		currentProfile = &Profile{
			Name:       profile.Name,
			Mode:       "AK",
			DisableSSL: new(bool),
		}
		*currentProfile.DisableSSL = false
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
	} else {
		currentProfile.Endpoint = volcengineutil.NewEndpoint().GetEndpoint()
	}
	if profile.SessionToken != "" {
		currentProfile.SessionToken = profile.SessionToken
	}
	if profile.DisableSSL != nil {
		*currentProfile.DisableSSL = *profile.DisableSSL
	}

	cfg.Profiles[currentProfile.Name] = currentProfile
	cfg.Current = currentProfile.Name
	return WriteConfigToFile(cfg)
}

func getConfigProfile(profileName string) error {
	var (
		exist          bool
		currentProfile *Profile
		cfg            *Configure
	)

	// if config not exist, return
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

	// if config not exist, return
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

	// if config not exist, return error
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
