#!/bin/sh
set -eu
ROOT=/data/prime-unlimited
exec "$ROOT/bin/prime-unlimited-runtime-api" \
    -listen 127.0.0.1:8098 \
    -model JC11S \
    -firmware 5.3.0 \
    -catalog "$ROOT/catalog/catalog.json"
