.PHONY: build link unlink

build:
	go build -o bin/herdr-pane-title .

link: build
	herdr plugin link $(CURDIR)

unlink:
	herdr plugin unlink zee-sh.pane-title
