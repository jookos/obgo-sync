#!/bin/sh

echo "Bridging 0.0.0.0:8181 -> 127.0.0.1:8180"
socat TCP4-LISTEN:8181,fork,reuseaddr TCP4:127.0.0.1:8180 &

qmd collection add /data/vault --name obsidian
qmd embed
qmd $@
