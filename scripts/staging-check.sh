#!/bin/bash

set -euo pipefail

STAGING="/mnt/staging"
QUARANTINE="/mnt/staging/.quarantine"
PERMANENT="/mnt/14tb_sata_1/media"
NTFY_URL="https://ntfy.sh/saturn-rips"
LOG="/var/log/staging-watcher/check.log"

log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') $*" | tee -a "$LOG"
}

notify() {
    local title="$1"
    local msg="$2"
    curl -s -d "$msg" -H "Title: $title" "$NTFY_URL" > /dev/null
}

check_file() {
    local filepath="$1"
    local filename
    filename="$(basename "$filepath")"

    log "Processing: $filename"

    # 1. MIME type check (cheap — runs first)
    local mime
    mime="$(file --mime-type -b "$filepath")"
    log "MIME type: $mime"

    local allowed_mimes=(
        "video/x-matroska"
        "video/mp4"
        "video/x-msvideo"
        "video/mpeg"
        "application/x-bittorrent"
        "application/zip"
        "application/gzip"
        "application/x-tar"
        "text/plain"
    )

    local mime_ok=false
    for allowed in "${allowed_mimes[@]}"; do
        if [[ "$mime" == "$allowed" ]]; then
            mime_ok=true
            break
        fi
    done

    if [[ "$mime_ok" == false ]]; then
        log "FAIL: Unexpected MIME type '$mime' for $filename"
        mv "$filepath" "$QUARANTINE/"
        notify "Staging: MIME Fail" "$filename blocked — unexpected type: $mime"
        return 1
    fi

    # 2. ClamAV scan (expensive — runs second)
    if ! clamscan --quiet "$filepath"; then
        log "FAIL: ClamAV flagged $filename"
        mv "$filepath" "$QUARANTINE/"
        notify "Staging: AV Fail" "$filename flagged by ClamAV"
        return 1
    fi

    # 3. Checksum before move
    local hash_before
    hash_before="$(sha256sum "$filepath" | awk '{print $1}')"

    # 4. Mirror relative path into permanent storage
    local relpath="${filepath#"$STAGING"/}"
    local destpath="$PERMANENT/$relpath"
    mkdir -p "$(dirname "$destpath")"
    mv "$filepath" "$destpath"

    # 5. Verify checksum after move
    local hash_after
    hash_after="$(sha256sum "$destpath" | awk '{print $1}')"

    if [[ "$hash_before" != "$hash_after" ]]; then
        log "FAIL: Checksum mismatch after move for $filename"
        mv "$destpath" "$STAGING/.quarantine"
        notify "Staging: Checksum Fail" "$filename may be corrupted — checksums differ. Quarantining file."
        return 1
    fi

    log "OK: $relpath -> $destpath"
}

if [[ $# -lt 1 ]]; then
    echo "Usage: staging-check.sh <filepath>"
    exit 1
fi

check_file "$1"

# 6. Clean up empty directories left behind in staging
find "$STAGING" -mindepth 1 -type d -empty -not -path "$STAGING/.*" | while read -r d; do
    rmdir "$d"
    log "Removed empty dir: $d"
done
find "$STAGING" -depth -type d -empty -wholename "$STAGING/.*/*" | while read -r d; do
    rmdir "$d"
    log "Removed empty dir: $d"
done