# Add moving service file?
run: build stopService
	./bin/watermeter

build:
	./dev/build.sh

startService:
	echo "Starting service"
	sudo systemctl start watermeter

stopService:
	echo "Stopping service"
	sudo systemctl stop watermeter
