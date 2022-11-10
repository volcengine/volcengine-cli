package cli

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
)

const CONFIG_FILE_DIR = "/usr/local/.volcengine/"
const CONFIG_FILE = "config.json"

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
	_, err := os.Stat(CONFIG_FILE_DIR)
	if err != nil {
		if os.IsNotExist(err) {
			os.MkdirAll(CONFIG_FILE_DIR, 0666)
		}
	}

	_, err = os.Stat(CONFIG_FILE_DIR + CONFIG_FILE)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
	}

	file, err := os.Open(CONFIG_FILE_DIR + CONFIG_FILE)
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
	file, err := os.OpenFile(CONFIG_FILE_DIR+CONFIG_FILE, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.Encode(config)
	return nil
}

func (p Profile) String() string {
	b, _ := json.MarshalIndent(p, "", "    ")
	return string(b)
}
