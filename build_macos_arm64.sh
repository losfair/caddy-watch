#!/bin/sh

set -e

PKG_CONFIG_PATH=/opt/homebrew/Cellar/openssl@1.1/1.1.1l_1/lib/pkgconfig go build -tags dynamic -o caddy-watch ./main.go

