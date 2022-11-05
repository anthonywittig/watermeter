set -e

DB_USER=`grep DB_USER= .env | sed 's/DB_USER=//'`
DB_PASSWORD_HASH=`grep DB_PASSWORD_HASH= .env | sed 's/DB_PASSWORD_HASH=//'`
DB_NAME=`grep DB_NAME= .env | sed 's/DB_NAME=//'`

#docker run --net=host -e DATA_SOURCE_NAME="postgresql://${DB_USER}:${DB_PASSWORD_HASH}@localhost:5432/${DB_NAME}?sslmode=disable" wrouesnel/postgres_exporter

# On RPI

#DATA_SOURCE_NAME="postgresql://${DB_USER}:${DB_PASSWORD_HASH}@localhost:5432/${DB_NAME}?sslmode=disable" ; /home/pi/go/bin/postgres_exporter 
#DATA_SOURCE_NAME="user=${DB_USER} password=${DB_PASSWORD_HASH} host=localhost sslmode=disable" ; /home/pi/go/bin/postgres_exporter 
#DATA_SOURCE_NAME="user=${DB_USER} password=${DB_PASSWORD_HASH} host=localhost sslmode=disable" ; /home/pi/go/bin/postgres_exporter 


sudo -u postgres DATA_SOURCE_NAME="user=postgres host=/var/run/postgresql/ sslmode=disable" ~/go/bin/postgres_exporter
