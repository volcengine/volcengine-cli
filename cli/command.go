package cli

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/volcengine/volcengine-cli/util"
)

type Command struct {
	Name        string
	subCommands []*Command
	flags       *FlagSet
	Run         func(ctx *Context, args []string) error
	rootSupport *RootSupport
}

func NewRootCommand() *Command {
	return &Command{
		Name: "volcengine",
	}
}

func (c *Command) InitRootCommand() {
	c.Run = c.RootCommandRun
	c.rootSupport = NewRootSupport()
}

func (c *Command) RootCommandRun(ctx *Context, args []string) (err error) {
	if len(args) >= 1 && args[0] == "configure" {
		return c.HandleConfigureCommand(ctx, args)
	}

	//检查参数
	if len(args) != 2 && len(ctx.dynamicFlags.GetFlags()) > 0 {
		err = fmt.Errorf("parse failed: -- must set after service and action")
		return
	}

	//列表列出所有支持的服务
	if len(args) == 0 {
		for _, svc := range c.rootSupport.GetAllSvc() {
			util.Cyan().Println(svc)
		}
	}

	//列出当前服务下的Action
	if len(args) == 1 {
		if !c.rootSupport.IsValidSvc(args[0]) {
			err = fmt.Errorf("%s is unsupport product", args[0])
			return
		}
		for _, action := range c.rootSupport.GetAllAction(args[0]) {
			util.Magenta().Println(action)
		}
	}

	if len(args) == 2 {
		if !c.rootSupport.IsValidAction(args[0], args[1]) {
			err = fmt.Errorf("%s.%s is unsupport action", args[0], args[1])
			return
		}
		var (
			sdk *SdkClient
			out *map[string]interface{}
		)
		sdk, err = NewSimpleClient(ctx)
		if err != nil {
			return
		}

		method := "GET"
		contentType := ""
		serviceName := args[0]
		apiInfo := c.rootSupport.GetApiInfo(args[0], args[1])

		if apiInfo != nil && apiInfo.Method != "" {
			method = apiInfo.Method
		}

		if apiInfo != nil && apiInfo.ContentType != "" {
			contentType = apiInfo.ContentType
		}

		input := make(map[string]interface{})
		for _, f := range ctx.dynamicFlags.flags {
			// rebuild input
			if f.Name != "body" {
				if a, success := util.ParseToJsonArrayOrObject(strings.TrimSpace(f.value)); success {
					input[f.Name] = a
				} else {
					input[f.Name] = f.value
				}
			} else {
				// origin
				if apiInfo.ContentType == "application/json" {
					err = json.Unmarshal([]byte(f.value), &input)
					if err != nil {
						return err
					}
					break
				} else {
					input[f.Name] = f.value
				}

			}
		}

		if svc, ok := GetServiceMapping(serviceName); ok {
			serviceName = svc
		}

		if strings.ToLower(contentType) != "application/json" {
			out, err = sdk.CallSdk(SdkClientInfo{
				ServiceName: serviceName,
				Action:      args[1],
				Version:     c.rootSupport.GetVersion(args[0]),
				Method:      method,
				ContentType: contentType,
			}, &input)
		} else {
			if jsonStr, ok := input["body"]; ok {
				var (
					a []interface{}
				)
				m := make(map[string]interface{})
				err = json.Unmarshal([]byte(jsonStr.(string)), &m)
				if err != nil {
					err = json.Unmarshal([]byte(jsonStr.(string)), &a)
					if err != nil {
						fmt.Println("json format error")
						return
					}
					out, err = sdk.CallSdk(SdkClientInfo{
						ServiceName: serviceName,
						Action:      args[1],
						Version:     c.rootSupport.GetVersion(args[0]),
						Method:      method,
						ContentType: contentType,
					}, &a)
				} else {
					out, err = sdk.CallSdk(SdkClientInfo{
						ServiceName: serviceName,
						Action:      args[1],
						Version:     c.rootSupport.GetVersion(args[0]),
						Method:      method,
						ContentType: contentType,
					}, &m)
				}
			} else {
				out, err = sdk.CallSdk(SdkClientInfo{
					ServiceName: serviceName,
					Action:      args[1],
					Version:     c.rootSupport.GetVersion(args[0]),
					Method:      method,
					ContentType: contentType,
				}, &input)
			}
		}

		b, _ := json.MarshalIndent(out, "", "\t")
		fmt.Println(string(b))
	}
	return
}

func (c *Command) HandleConfigureCommand(ctx *Context, args []string) (err error) {
	if len(args) == 2 {
		switch args[1] {
		case "set":
			return setConfigProfile(ctx)
		case "get":
			return getConfigProfile(ctx)
		case "list":

		case "delete":
			return deleteConfigProfile(ctx)
		default:
			return fmt.Errorf("%v is not a valid command", args[1])
		}
	}
	return nil
}

func setConfigProfile(ctx *Context) error {
	var (
		profileFlag    *Flag
		exist          bool
		currentProfile *Profile
		cfg            *Configure
	)

	// check profile and region
	if profileFlag, exist = ctx.dynamicFlags.index["--profile"]; !exist {
		return fmt.Errorf("please set profile name")
	}
	if _, exist = ctx.dynamicFlags.index["--region"]; !exist {
		return fmt.Errorf("please set region")
	}

	// if config not exist, create an empty config
	if cfg = ctx.config; cfg == nil {
		cfg = &Configure{
			Profiles: make(map[string]*Profile),
		}
	}

	// check if the target profile already exists
	// otherwise create a new profile
	if currentProfile, exist = cfg.Profiles[profileFlag.value]; !exist {
		currentProfile = &Profile{
			Name: profileFlag.value,
			Mode: "AK",
		}
	}

	for _, f := range ctx.dynamicFlags.flags {
		switch f.Name {
		case "access-key":
			currentProfile.AccessKey = f.value
		case "secret-key":
			currentProfile.SecretKey = f.value
		case "region":
			currentProfile.Region = f.value
		}
	}

	if !exist {
		cfg.Profiles[currentProfile.Name] = currentProfile
	}
	cfg.Current = currentProfile.Name
	return WriteConfigToFile(cfg)
}

func getConfigProfile(ctx *Context) error {
	var (
		profileFlag    *Flag
		exist          bool
		currentProfile *Profile
		cfg            *Configure
	)

	// check profile flag
	if profileFlag, exist = ctx.dynamicFlags.index["--profile"]; !exist {
		return fmt.Errorf("please provide profile name")
	}

	// check invalid flag
	for _, f := range ctx.dynamicFlags.flags {
		if f.Name != "profile" {
			return fmt.Errorf("invalid flag %v", f.Name)
		}
	}

	// if config not exist, return
	if cfg = ctx.config; cfg == nil {
		fmt.Println(Profile{})
		return nil
	}

	// check if the target profile already exists, otherwise print an empty profile
	if currentProfile, exist = cfg.Profiles[profileFlag.value]; !exist || currentProfile == nil {
		currentProfile = &Profile{}
	}
	fmt.Println(*currentProfile)
	return nil
}

func deleteConfigProfile(ctx *Context) error {
	var (
		profileFlag *Flag
		exist       bool
		cfg         *Configure
	)

	// check profile flag
	if profileFlag, exist = ctx.dynamicFlags.index["--profile"]; !exist {
		return fmt.Errorf("please set profile name")
	}

	// if config not exist, return error
	if cfg = ctx.config; cfg == nil {
		return fmt.Errorf("configuration profile %v not found", profileFlag.value)
	}

	// check if the target profile exists
	if _, exist = cfg.Profiles[profileFlag.value]; !exist {
		return fmt.Errorf("configuration profile %v not found", profileFlag.value)
	}

	// delete profile and write change to config file
	delete(cfg.Profiles, profileFlag.value)
	return WriteConfigToFile(cfg)
}

func (c *Command) GetFlags() *FlagSet {
	if c.flags == nil {
		c.flags = NewFlagSet()
	}
	return c.flags
}

func (c *Command) Execute(ctx *Context, args []string) error {
	parser := NewParser(args)

	param, err := parser.ReadArgs(ctx)
	if err != nil {
		return fmt.Errorf("parse failed %s", err)
	}
	return c.Run(ctx, param)
}
