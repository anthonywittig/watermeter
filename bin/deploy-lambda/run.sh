#!/bin/bash

set -ex

scriptDir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
cd $scriptDir

profile="$1"
lambdaName="$2"
lambdaDir=$(realpath "../../lambdas/cmd/$lambdaName")
buildDir="../../build/${lambdaName}"

cd "../../lambdas/"
go generate ./...
go vet ./...
go test ./...

cd $scriptDir
mkdir -p $buildDir
rm -r $buildDir
mkdir $buildDir
buildDirAbs=$(realpath "${buildDir}")
# Might need to copy some config here.
#if test -f "${lambda}/.env"; then
#    cp ${lambda}/.env $buildDir
#fi

(cd $lambdaDir && GOOS=linux GOARCH=amd64 go build -o $buildDirAbs main.go)
(cd $buildDirAbs && zip -r function.zip *) # Might need to include .env at some point.

go build -o main
./main $profile $lambdaName
