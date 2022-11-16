package util

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"encoding/json"
	"os/user"
	"strings"
)

func IsRepeatedField(f string) bool {
	return strings.Contains(f, ".N")
}

func IsJsonArray(value string) bool {
	return len(value) >= 2 && value[0] == '[' && value[len(value)-1] == ']'
}

//ParseToJsonArrayOrObject try to parse string to json array or json object
func ParseToJsonArrayOrObject(s string) (interface{}, bool) {
	if !json.Valid([]byte(s)) || len(s) < 2 {
		return nil, false
	}

	var a interface{}
	if (s[0] == '[' && s[len(s)-1] == ']') || (s[0] == '{' && s[len(s)-1] == '}') {
		if err := json.Unmarshal([]byte(s), &a); err != nil {
			return err, false
		} else {
			return a, true
		}
	}
	return nil, false
}

func GetConfigFileDir() (string, error) {
	var (
		err     error
		homeDir string
	)

	if homeDir, err = getHomeDir(); err != nil {
		return "", err
	}

	return homeDir + "/.volcengine/", nil
}

func getHomeDir() (string, error) {
	user, err := user.Current()
	if err != nil {
		return "", err
	}

	return user.HomeDir, nil
}
