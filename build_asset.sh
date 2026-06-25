#!/bin/bash
url=$1
urlBranch=$2

if [ "$url" == "" ]
then
  echo 'Please set metadata repo url'
  exit
fi

#clean git cache before build
rm -rf volcengine-sdk-metadata
rm -rf .git/modules/volcengine-sdk-metadata
git config --local --unset submodule.volcengine-sdk-metadata.url
git config --local --unset submodule.volcengine-sdk-metadata.active
git rm --cached volcengine-sdk-metadata

rm -rf .gitmodules
touch .gitmodules

git submodule add "$url" volcengine-sdk-metadata
if [ "$urlBranch" != "" ]
then
 cd volcengine-sdk-metadata
 git checkout -b "$urlBranch" origin/"$urlBranch"
 cd ..
fi
if ! go run ./scripts/generate_explorer_descriptions.go --metadata-dir volcengine-sdk-metadata/metadata --out volcengine-sdk-metadata/explorer_descriptions/descriptions.json
then
  echo "skip explorer descriptions generation"
  mkdir -p volcengine-sdk-metadata/explorer_descriptions
  printf '{}\n' > volcengine-sdk-metadata/explorer_descriptions/descriptions.json
fi

go-bindata -pkg asset  -o asset/asset.go volcengine-sdk-metadata/metadata/... volcengine-sdk-metadata/explorer_descriptions/...
go-bindata -pkg typeset  -o typeset/typeset.go volcengine-sdk-metadata/metatype/...
go-bindata -pkg structset  -o structset/structset.go volcengine-sdk-metadata/structure/...


#clean git cache after build
rm -rf volcengine-sdk-metadata
rm -rf .git/modules/volcengine-sdk-metadata
git config --local --unset submodule.volcengine-sdk-metadata.url
git config --local --unset submodule.volcengine-sdk-metadata.active
git rm --cached volcengine-sdk-metadata

rm -rf .gitmodules

