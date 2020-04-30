set -e

mkdir -p bin
rm -rf bin
mkdir -p bin

go build -o "bin/watermeter" main.go
ln -s "$(pwd)/.env" bin
