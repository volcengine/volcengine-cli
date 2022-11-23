#!/bin/bash
OS=$1

set -ea

if [ "$OS" == "" ]
then
  OS="darwin"
fi

NAME="volcengine-cli-"$OS

CGO_ENABLED=0 GOOS=$OS GOARCH=amd64 go build  -o $NAME -tags codegen