.PHONY: build link unlink restart

build:
	go build -o bin/herdr-pane-title .

# Relink after manifest changes; restart picks up a rebuilt binary.
link: build
	-herdr plugin unlink zee-sh.pane-title >/dev/null 2>&1
	herdr plugin link $(CURDIR)
	$(MAKE) restart

restart: build
	-pkill -f 'herdr-pane-title watch'
	herdr plugin action invoke refresh --plugin zee-sh.pane-title >/dev/null

unlink:
	-pkill -f 'herdr-pane-title watch'
	herdr plugin unlink zee-sh.pane-title
