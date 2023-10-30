clean:
	-rm -r ./build

build:
	go build -o ./build/migrate
