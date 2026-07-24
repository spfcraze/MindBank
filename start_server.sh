#!/bin/bash
cd /home/rat/mindbank
export MB_DB_DSN="postgres://mindbank:mindbank@172.18.0.2:5432/mindbank?sslmode=disable"
export MB_PORT=8095
./mindbank >> mindbank.log 2>&1 &
echo "MindBank started with PID: $!"
