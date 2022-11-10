#!/bin/bash
OS=$1
NAME="volcengine-cli"
set -ea

if [ "$OS" == "" ]
then
  OS="darwin"
fi

CGO_ENABLED=0 GOOS=$OS GOARCH=amd64 go build  -o $NAME -tags codegen