package main

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"os"

	"github.com/volcengine/volcengine-cli/cli"
)

func main() {
	args := os.Args

	rootCommand := cli.NewRootCommand()
	rootCommand.InitRootCommand()
	config := cli.LoadConfig()
	ctx := cli.NewContext()
	ctx.SetCommand(rootCommand)
	ctx.SetConfig(config)
	err := rootCommand.Execute(ctx, args[1:])
	if err != nil {
		fmt.Println(err)
	}
}
