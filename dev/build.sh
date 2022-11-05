set -e

mkdir -p bin
rm -rf bin
mkdir -p bin

cd rpi
go build -o "../bin/watermeter" main.go
cd ../

cp ../watermeter-config/rpi/.env bin/
