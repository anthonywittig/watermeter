#!/bin/bash

set -ex

scriptDir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
cd $scriptDir

profile="$1"
if [[ -z "$profile" ]]; then
    echo "Missing profile."
    exit 1
fi
lambdaName="$2"
if [[ -z "$lambdaName" ]]; then
    echo "Missing lambda name."
    exit 1
fi
token="$3"
if [[ -z "$token" ]]; then
    echo "Missing token."
    exit 1
fi
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
(cd $lambdaDir && GOOS=linux GOARCH=amd64 go build -o $buildDirAbs main.go)
(cd $buildDirAbs && zip -r function.zip *)

go build -o main
./main $profile $lambdaName $token
