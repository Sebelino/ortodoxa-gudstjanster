#!/bin/bash
set -e

CACHE_DIR="${HOME}/.cache/ortodoxa-gudstjanster"

if [ -d "$CACHE_DIR" ]; then
    rm -rf "$CACHE_DIR"/*
    echo "Cleared disk cache: $CACHE_DIR"
else
    echo "Cache directory does not exist: $CACHE_DIR"
fi

echo ""
echo "To clear browser cache, open DevTools (F12) and:"
echo "  - Hard refresh: Cmd+Shift+R (Mac) or Ctrl+Shift+R (Windows/Linux)"
echo "  - Or: Network tab → Right-click → Clear browser cache"
