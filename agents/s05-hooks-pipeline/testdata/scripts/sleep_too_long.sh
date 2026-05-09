#!/bin/sh
# sleep_too_long.sh — burns 30 wall-clock seconds. Used by the timeout
# test: the manifest gives this hook a 1-second budget, so the
# dispatcher must kill it via context cancellation long before sleep
# returns on its own.
sleep 30
