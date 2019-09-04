all: build

build:
	mkdir -p out/bin
	go build -o out/bin/deepin-ab-recovery

install:

clean:
	rm -rf out
