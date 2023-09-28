#!/usr/bin/env bash

if [[ -z "$1" ]]; then
    echo "Usage: $0 tag"
    echo "E.g.:  $0 0.0.1"
    exit 1
fi

tag="$1"

KO_DOCKER_REPO=harbor.powerbot.energy/utilities ko build --tags="$tag" --base-import-paths .