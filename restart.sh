#!/bin/bash
pkill -f mindbank-api
sleep 1
cd /home/rat/mindbank
export MB_DB_DSN="postgres://mindbank:mindbank_secret@localhost:5434/mindbank?sslmode=disable"
./mindbank-api >> mindbank.log 2>&1 &
echo "started, PID: $!"
