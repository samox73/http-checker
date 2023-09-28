#!/usr/bin/env bash

KO_DOCKER_REPO=harbor.powerbot.energy/utilities ko build --tags=0.0.1 --platform=linux/amd64,linux/arm64 --base-import-paths .