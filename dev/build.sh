set -e

mkdir -p bin
rm -rf bin
mkdir -p bin

cd rpi
go build -o "../bin/watermeter" main.go
cd ../

cp ../watermeter-config/config/rpi/.env bin/

# Firebase service-account key for the Firestore-based valve control. Its
# deployed path is what FIREBASE_CREDENTIALS in .env should point at.
cp ../watermeter-config/config/firebase/service-account.json bin/
