set -e

# Need to run a sudo.

# These aren't needed?
#mkdir -p /etc/prometheus
#cp prometheus.yml /etc/prometheus/
#mkdir -p /prometheus

# Stop any running containers, will exit with error if not running.

running_container=$(docker ps -q --filter ancestor=prom/prometheus)
if [ -n "$running_container" ]; then
	echo "Stopping running container ${running_container}..."
	docker stop ${running_container}
fi

echo "Starting container..."
# docker run -d -p 9090:9090 -v ~/prometheus.yml:/etc/prometheus/prometheus.yml prom/prometheus -config.file=/etc/prometheus/prometheus.yml -storage.local.path=/prometheus -storage.local.memory-chunks=10000
docker run -d --network="host" -v $(pwd)/prometheus.yml:/etc/prometheus/prometheus.yml prom/prometheus --config.file=/etc/prometheus/prometheus.yml --storage.tsdb.path=/prometheus
