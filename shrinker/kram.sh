#!/usr/bin/env bash
#
cd "$(dirname "$0")" || exit 4
pwd

go get "golang.org/x/sync/errgroup" || exit 5

go run . kram "$@"
