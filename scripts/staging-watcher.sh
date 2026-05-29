#!/usr/bin/env bash

set -euo pipefail

STAGING="/mnt/staging"

inotifywait -m -r -e close_write -e moved_to --format '%w%f' "$STAGING" | while read -r filepath; do
    filename="$(basename "$filepath")"

    [[ "$filepath" == */.* ]] && continue
    [[ "$filename" == *.log ]] && continue

    bash /home/saturn-svc/scripts/staging-check.sh "$filepath" &
done
