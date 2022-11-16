package cli

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"encoding/json"
	"fmt"
	"github.com/volcengine/volcengine-cli/util"
	"io/ioutil"
	"os"
)

const ConfigFile = "config.json"

type Configure struct {
	Current  string              `json:"current"`
	Profiles map[string]*Profile `json:"profiles"`
}

type Profile struct {
	Name         string `json:"name"`
	Mode         string `json:"mode"`
	AccessKey    string `json:"access-key"`
	SecretKey    string `json:"secret-key"`
	Region       string `json:"region"`
	Endpoint     string `json:"endpoint"`
	SessionToken string `json:"session-token"`
	DisableSSL   bool   `json:"disable-ssl"`
}

//LoadConfig from CONFIG_FILE_DIR
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

//WriteConfigToFile store config
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

func (p Profile) String() string {
	b, _ := json.MarshalIndent(p, "", "    ")
	return string(b)
}
