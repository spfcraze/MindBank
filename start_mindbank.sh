#!/bin/bash
cd /home/rat/mindbank
# Source .env if it exists
if [ -f .env ]; then
    set -a
    source .env
    set +a
fi
export MB_PORT=8095
./mindbank-api >> mindbank.log 2>&1 &
echo $! > /tmp/mindbank-api.pid
echo "MindBank API started with PID: $!"
