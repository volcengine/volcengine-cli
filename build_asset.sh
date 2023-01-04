#!/bin/bash
url=$1
typeUrl=$2

if [ "$url" == "" ]
then
  echo 'Please set metadata repo url'
  exit
fi

if [ "$typeUrl" == "" ]
then
  echo 'Please set metatype repo url'
  exit
fi

#clean git cache before build
rm -rf volcengine-sdk-metadata
rm -rf .git/modules/volcengine-sdk-metadata
git config --local --unset submodule.volcengine-sdk-metadata.url
git config --local --unset submodule.volcengine-sdk-metadata.active
git rm --cached volcengine-sdk-metadata

rm -rf volcengine-sdk-metatype
rm -rf .git/modules/volcengine-sdk-metatype
git config --local --unset submodule.volcengine-sdk-metatype.url
git config --local --unset submodule.volcengine-sdk-metatype.active
git rm --cached volcengine-sdk-metatype

rm -rf .gitmodules
touch .gitmodules

git submodule add "$url" volcengine-sdk-metadata
go-bindata -pkg asset  -o asset/asset.go volcengine-sdk-metadata/...

git submodule add "$typeUrl" volcengine-sdk-metatype
go-bindata -pkg typeset  -o typeset/typeset.go volcengine-sdk-metatype/...

#clean git cache after build
rm -rf volcengine-sdk-metadata
rm -rf .git/modules/volcengine-sdk-metadata
git config --local --unset submodule.volcengine-sdk-metadata.url
git config --local --unset submodule.volcengine-sdk-metadata.active
git rm --cached volcengine-sdk-metadata

rm -rf volcengine-sdk-metatype
rm -rf .git/modules/volcengine-sdk-metatype
git config --local --unset submodule.volcengine-sdk-metatype.url
git config --local --unset submodule.volcengine-sdk-metatype.active
git rm --cached volcengine-sdk-metatype

rm -rf .gitmodules

