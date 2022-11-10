#!/bin/bash
url=$1
if [ "$url" == "" ]
then
  echo 'Please set metadata repo url'
else
  #clean git cache before build
  rm -rf volcengine-sdk-metadata
  rm -rf .git/modules/volcengine-sdk-metadata
  git config --local --unset submodule.volcengine-sdk-metadata.url
  git config --local --unset submodule.volcengine-sdk-metadata.active
  git rm --cached volcengine-sdk-metadata
  rm -rf .gitmodules
  touch .gitmodules

  git submodule add "$url" volcengine-sdk-metadata
  go-bindata -pkg asset  -o asset/asset.go volcengine-sdk-metadata/...

  #clean git cache after build
  rm -rf volcengine-sdk-metadata
  rm -rf .git/modules/volcengine-sdk-metadata
  git config --local --unset submodule.volcengine-sdk-metadata.url
  git config --local --unset submodule.volcengine-sdk-metadata.active
  git rm --cached volcengine-sdk-metadata
  rm -rf .gitmodules
fi

