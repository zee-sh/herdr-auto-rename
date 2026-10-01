.PHONY: build link restart unlink

ID := zee-sh.auto-rename

build:
	go build -o bin/herdr-auto-rename .

# (Re)link after manifest changes, then hand over to the new binary.
link: build
	-herdr plugin unlink $(ID) >/dev/null 2>&1
	herdr plugin link $(CURDIR) >/dev/null
	$(MAKE) restart

restart: build
	herdr plugin action invoke restart --plugin $(ID) >/dev/null

unlink:
	-pkill -f 'herdr-auto-rename watch'
	herdr plugin unlink $(ID)
