# Add moving service file?
run: build stopService
	./bin/watermeter

build:
	./dev/build.sh

startService:
	echo "Starting service"
	sudo systemctl restart watermeter

stopService:
	echo "Stopping service"
	sudo systemctl stop watermeter

deploy-lambdas:
	./bin/deploy-lambda/run.sh watermeter-deployer-role inbound-text $(token)
