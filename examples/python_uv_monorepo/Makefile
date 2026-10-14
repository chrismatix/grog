.PHONY: build test lint images

build:
	grog build //...

test:
	grog test //...

lint:
	grog build '//...:lint'

images:
	grog build //server:image //cli:image
