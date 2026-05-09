#!/bin/sh
# echo.sh — copy stdin to stdout. The dispatcher pipes the JSON payload
# in; we relay it back so tests can assert on what the hook saw.
cat
