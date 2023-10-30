DB_CONN := $(bat .env | rg DB_CONN | sed 's/^[^=]*=//')
MIGRATION_FOLDER := ./migrations

clean:
	-rm -r ./build

build:
	go build -o ./build/migrate
