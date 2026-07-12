#!/bin/bash
set -uo pipefail

# === Helper functions ===
    exit_handler() {
        local msg="$1" phase
        EXIT_CODE="${2:-255}"

        # Error exit codes:
        # 1 -> Pre-flight error
        # 2 -> No disc in drive error
        # 3 -> Disc error
        # 4 -> Capacity error
        # 5 -> MakeMKV error
        # 6 -> File indexing error

        if (( EXIT_CODE == 0 )); then
            phase="Complete!"
        else
            phase="Failed [$EXIT_CODE]"
        fi

        echo -e "[$EXIT_CODE] $msg"
        [[ -n "${NTFY_URL:-}" ]] && curl -sd "[${DRIVE_TAG:-?}] [$EXIT_CODE] $msg" "$NTFY_URL"

        if (( EXIT_CODE == 1 )) || (( EXIT_CODE == 130 )); then eject_flag=false; fi
        if $eject_flag && drive_state; then
            echo "Ejecting disc ..."
            eject "$DEVICE"
        fi

        local now elapsed minutes seconds
        now=$(date +%s)
        elapsed=$((now-START))
        minutes=$((elapsed/60))
        seconds=$((elapsed%60))
        echo "End: $(date)"
        echo "Total Runtime: $minutes:$seconds"
        
        write_status "$phase"

        echo "Creating record ..."
        ripper record "$DRIVE_TAG" # Internal cobra command -> ripper/cmd/dbcmd.go

        local log_dest
        if [[ -n "$DEST_REL" ]]; then
            log_dest="$PERMANENT/$DEST_REL/Logs/$RAW_TITLE.rip.log"
            mkdir -p "$(dirname "$log_dest")"
            if [[ -e "$log_dest" ]]; then
                local j=1
                while [[ -e "$PERMANENT/$DEST_REL/Logs/$RAW_TITLE(${j}).rip.log" ]]; do
                    ((j++))
                done
                log_dest="$PERMANENT/$DEST_REL/Logs/$RAW_TITLE(${j}).rip.log"
            fi
        else
            local ts fallback_dir
            ts=$(date +"%Y-%m-%d_%H-%M-%S")
            fallback_dir="${PERMANENT:-/mnt/14tb_sata_1/media}/failure_logs"
            mkdir -p "$fallback_dir"
            log_dest="$fallback_dir/$ts.rip.log"
        fi
        echo "Exporting log to $log_dest ..."
        cp "$LOG" "$log_dest"

        echo "merciful bliss ..."
        exit "$EXIT_CODE"
    }

    # shellcheck disable=SC2329
    on_signal() {
        trap '' INT TERM HUP QUIT
        echo ""
        echo "Signal received, cleaning up ..."
        for p in "${MKV_PID:-}" "${rsync_pid:-}"; do
            [[ -n "$p" ]] && kill -TERM "$p" 2>/dev/null
        done
        sleep 2
        for p in "${MKV_PID:-}" "${rsync_pid:-}"; do
            [[ -n "$p" ]] && kill -KILL "$p" 2>/dev/null
        done
        exit_handler "Interrupted by signal" 130
    }

    drive_state() {
        local drive="${1:-$DEVICE}"
        udevadm info --query=property --name="$drive" | grep -q '^ID_CDROM_MEDIA=1$'
    }

    safe_du() {
        local path="$1"
        local fallback="${2:-0}"
        local result
        result=$(timeout 5 du -sm "$path" 2>/dev/null | awk 'NR{print $1+0; exit}')
        echo "${result:-$fallback}"
    }

    staging_avail() {
        df --output=avail -BM "$STAGING" | tail -n1 | tr -dc '0-9'
    }

    write_status() {
        local phase="$1"
        local now elapsed_seconds full_dest

        [[ -z "${STATUS:-}" ]] && return 0

        now=$(date +%s)
        elapsed_seconds=$((now - START))

        case "$phase" in 
            "Starting ...")
                full_dest="$STAGING" ;;
            "Ripping ...")
                full_dest="$BATCH_DIR" ;;
            "Waiting ...")
                full_dest="$BATCH_DIR" ;;
            "Scanning ...")
                full_dest="$STAGE_DIR" ;;
            "Moving ...")
                full_dest="$PERM_DIR" ;;
            "Complete!")
                full_dest="$PERM_DIR" ;;
            *)
                full_dest="$PERMANENT" ;;
        esac

        jq -n \
            --argjson start "$START" \
            --argjson run_pid "$$" \
            --argjson mkv_pid "${MKV_PID:-0}" \
            --arg drive "$DRIVE_TAG" \
            --arg device "$DEVICE" \
            --arg phase "$phase" \
            --arg raw_title "${RAW_TITLE:-UNKNOWN}" \
            --arg title "${TITLE:-UNKNOWN}" \
            --arg dest "${DEST_REL:-UNKNOWN}" \
            --arg full_dest "${full_dest:-UNKNOWN}" \
            --argjson cur_rip_mb "${CURRENT_RIP_MB:-0}" \
            --argjson cur_mv_mb "${CURRENT_MV_MB:-0}" \
            --argjson total_rip_mb "${TOTAL_MB:-0}" \
            --argjson total_mv_mb "${STAGE_START_SIZE:-0}" \
            --arg sel_tracks "${SEL_TRACKS:-NONE}" \
            --argjson elapsed_seconds "${elapsed_seconds:-0}" \
            --arg updated "$(date)" \
            --argjson updated_epoch "$now" \
            --arg rip_log "$(cat "$LOG")" \
            --argjson exit_code "${EXIT_CODE:-0}" \
            '{
                start: ($start | todate),
                start_epoch: $start,
                run_pid: $run_pid,
                mkv_pid: $mkv_pid,
                drive: $drive,
                device: $device,
                phase: $phase,
                raw_title: $raw_title,
                title: $title,
                dest: $dest,
                full_dest: $full_dest,
                cur_rip_mb: $cur_rip_mb,
                cur_mv_mb: $cur_mv_mb,
                total_rip_mb: $total_rip_mb,
                total_mv_mb: $total_mv_mb,
                sel_tracks: $sel_tracks,
                elapsed_seconds: $elapsed_seconds,
                updated: ($updated | todate),
                updated_epoch: $updated_epoch,
                rip_log: $rip_log,
                exit_code: $exit_code
            }' > "$STATUS.tmp" && mv "$STATUS.tmp" "$STATUS"
    }

    reserved_by_others() {
        local total=0 now f data phase upd t c remain
        now=$(date +%s)
        # Glob sibling status files: same base/ext as $STATUS, any tag.
        for f in $STATUS_TMP; do
            [[ -e "$f" ]] || continue
            [[ "$f" == "$STATUS" ]] && continue   # skip our own
            # Single jq call extracts all needed fields as tab-separated values.
            data=$(jq -r '[.phase // "", .updated_epoch // 0, .total_rip_mb // 0, .cur_rip_mb // 0] | @tsv' "$f" 2>/dev/null) || continue
            IFS=$'\t' read -r phase upd t c <<< "$data"
            case "${phase%% *}" in
                Ripping|Moving|Waiting) ;;
                *) continue ;;
            esac
            [[ "$upd" =~ ^[0-9]+$ ]] || upd=0
            if (( now - upd > RESERVE_STALE_SECS )); then
                echo "WARN: ignoring stale reservation from $(basename "$f") (phase=$phase, $((now - upd))s old)" >&2
                continue
            fi
            [[ "$t" =~ ^[0-9]+$ ]] || t=0
            [[ "$c" =~ ^[0-9]+$ ]] || c=0
            remain=$(( t - c ))
            (( remain < 0 )) && remain=0
            total=$(( total + remain ))
        done
        echo "$total"
    }

    sibling_in_starting() {
        local now f data phase upd
        now=$(date +%s)
        for f in $STATUS_TMP; do
            [[ -e "$f" ]] || continue
            [[ "$f" == "$STATUS" ]] && continue
            data=$(jq -r '[.phase // "", .updated_epoch // 0] | @tsv' "$f" 2>/dev/null) || continue
            IFS=$'\t' read -r phase upd <<< "$data"
            [[ "${phase%% *}" == "Starting" ]] || continue
            [[ "$upd" =~ ^[0-9]+$ ]] || upd=0
            (( now - upd > RESERVE_STALE_SECS )) && continue   # stale: don't block on it
            return 0
        done
        return 1
    }

    # --- MakeMKV beta-key handling ---
    fetch_makemkv_beta_key() {
        local html rc key
        html=$(curl -fsSL --connect-timeout 10 --max-time 25 \
                    --retry 3 --retry-delay 2 --retry-connrefused \
                    -A 'Mozilla/5.0 (X11; Linux x86_64)' -- "$MAKEMKV_KEY_URL")
        rc=$?
        (( rc != 0 )) && { echo "makemkv: forum fetch failed (curl exit $rc)" >&2; return 1; }

        key=$(printf '%s' "$html" | grep -oE 'T-[A-Za-z0-9_+/=@-]{40,}' | head -n1)
        [[ $key =~ ^T-[A-Za-z0-9_+/=@-]{40,}$ ]] || { echo "makemkv: no key parsed from page" >&2; return 1; }

        printf '%s\n' "$key"
    }

    apply_makemkv_key() {
        local key="$1"
        local conf="${HOME}/.MakeMKV/settings.conf"
        mkdir -p "${conf%/*}" && touch "$conf" || return 1
        { grep -v '^app_Key' "$conf"; printf 'app_Key = "%s"\n' "$key"; } > "${conf}.tmp" \
            && mv -- "${conf}.tmp" "$conf"
    }

    makemkv_key_expired_output() {
        grep -qE 'MSG:(5052|5055),' <<<"$1"
    }

    refresh_makemkv_key() {
        local key
        if key=$(fetch_makemkv_beta_key); then
            if apply_makemkv_key "$key"; then
                echo "MakeMKV beta key refreshed from forum."
            else
                echo "WARN: fetched beta key but could not write settings.conf; using existing key."
            fi
        else
            echo "WARN: could not fetch latest beta key; relying on existing key."
        fi
    }

# === Initialization ===
    START=$(date +%s)

    declare -a STAGED_PATHS=()

    eject_flag=false
    track_select=false

    deps=(makemkvcon jq file clamdscan rsync tree eject curl ts flock lsof sudo)
    for dep in "${deps[@]}"; do
        if ! command -v "$dep"; then
            exit_handler "Missing required dependency: $dep" 1
        fi
    done
    
    while getopts "et" opt; do
        case $opt in
            e) eject_flag=true ;;
            t) track_select=true ;;
            *) echo "[?] Invalid option: -$OPTARG"; exit 1 ;;
        esac
    done
    shift $((OPTIND - 1))

    PERMANENT="${1:-}"
    STAGING="${2:-}"
    STATUS_TMP="${3:-}"
    LOG_TMP="${4:-}"
    NTFY_URL="${5:-}"
    DRIVE_NUM="${6:-}"
    MEDIA="${7:-}"
    SEASON="${8:-}"

    DRIVE_TAG="sr$DRIVE_NUM"
    DEVICE="/dev/$DRIVE_TAG"    
    STATUS="${STATUS_TMP/\*/$DRIVE_TAG}"
    LOG="${LOG_TMP/\*/$DRIVE_TAG}"

    WAIT_POLL_SECS=10
    WAIT_MAX_SECS=7200
    RESERVE_STALE_SECS=120
    MAKEMKV_KEY_URL='https://forum.makemkv.com/forum/viewtopic.php?t=1053'

    DEST_REL=""
    
    exec > >(ts '%Y-%m-%d %H:%M:%S' | tee "$LOG") 2>&1
    echo "Run PID: $$"
    echo "Command Received: $0 $*"
    echo "Start: $(date)"
    echo "Drive: $DEVICE (tag: $DRIVE_TAG)"

    curl -sd "Rip Request Received" "$NTFY_URL"


# === Preflight ===
    write_status "Starting ..."
    trap on_signal INT TERM HUP QUIT

    if [[ ! -b "$DEVICE" ]]; then exit_handler "Device '$DEVICE' is not a block device." 1; fi

    if [[ -z "$MEDIA" ]]; then exit_handler "Missing type." 1; fi
    case "$MEDIA" in
        movie|show) ;;
        *) exit_handler "Argument must be 'movie' or 'show'." 1 ;;
    esac

    if [[ "$MEDIA" == "show" && -z "$SEASON" ]]; then exit_handler "Shows require a season number." 1; fi
    if [[ "$MEDIA" == "show" && ! "$SEASON" =~ ^[0-9]+$ ]]; then exit_handler "Season number must be an integer." 1; fi

    exec 200>"/var/lock/movie-ripper.$DRIVE_TAG.lock"
    if ! flock -n 200; then
        holders=$(lsof -t "/var/lock/movie-ripper.$DRIVE_TAG.lock" 2>/dev/null | tr '\n' ',' | sed 's/,$//')
        echo "Lock held by existing PID(s): ${holders:-UNKNOWN}"
        exit_handler "Error! Lock for $DRIVE_TAG held by existing processes. Exiting ..." 1
    fi

    if ! smart_result=$(sudo -n /root/scripts/smart_check.sh "$STAGING" 2>&1); then
        echo "WARNING: $smart_result"
        curl -sd "[$DRIVE_TAG] WARN: SMART check on staging - $smart_result" "$NTFY_URL"
    else
        echo "SMART check: $smart_result"
    fi

    if ! drive_state; then
        i=0
        while (( i < 3 )) && ! drive_state; do
            echo "No disc detected in drive. Retrying ..."
            sleep 30
            ((i++))
        done
        if ! drive_state; then
            exit_handler "No disc in drive." 2
        fi
    fi

    sleep 5
    UDEV_PROPS=$(udevadm info --query=property --name="$DEVICE")
    if ! grep -q '^ID_FS_TYPE=' <<<"$UDEV_PROPS"; then
        exit_handler "Disc unreadable; no recognizable filesystem. Exiting ..." 3
    fi

    RAW_TITLE=$(sed -n 's/^ID_FS_LABEL=//p' <<<"$UDEV_PROPS")
    [[ -z "$RAW_TITLE" ]] && RAW_TITLE="UNKNOWN-$DRIVE_TAG"
    echo "Provisional disc title (udev): $RAW_TITLE"
    write_status "Starting ..."


# === MakeMKV index resolution ===
    # SIMPLIFICATION (untested on this build): the disc:9999 enumeration below
    # exists only to map $DEVICE -> disc:N. MakeMKV also accepts a "dev:" source,
    # so this whole section can be dropped and every "disc:$MKV_INDEX" replaced
    # with "dev:$DEVICE" (info and mkv calls). RAW_TITLE would then match the DRV
    # line by device instead of index:
    #   RAW_TITLE=$(echo "$INFO_OUTPUT" | awk -F',' -v dev="\"$DEVICE\"" \
    #       '$1 ~ /^DRV:/ && $NF == dev {split($0,a,"\""); print a[4]; exit}')
    # Verify the DRV: line format under a dev: source before switching.

    echo "Refreshing MakeMKV beta key ..."
    refresh_makemkv_key

    echo "Resolving MakeMKV disc index for $DEVICE ..."
    write_status "Starting ..."
    DRV_LIST=$(makemkvcon -r --cache=1 info disc:9999 2>/dev/null)
    MKV_INDEX=$(echo "$DRV_LIST" \
        | grep '^DRV:' \
        | awk -F',' -v dev="\"$DEVICE\"" '$NF == dev {sub(/^DRV:/,"",$1); print $1; exit}')

    if [[ -z "$MKV_INDEX" ]]; then
        echo "DRV enumeration:"
        echo "$DRV_LIST" | grep '^DRV:' | grep -v ',"",""' || true
        exit_handler "Could not map $DEVICE to a MakeMKV disc index." 3
    fi
    echo "MakeMKV index for $DEVICE: disc:$MKV_INDEX"

# === Disc info, title parsing, and directory setting ===
    echo "Getting disc info ..."
    write_status "Starting ..."
    INFO_OUTPUT=$(makemkvcon -r info "disc:$MKV_INDEX")

    if makemkv_key_expired_output "$INFO_OUTPUT"; then
        exit_handler "MakeMKV beta key expired (MSG 5052/5055); disc cannot be decrypted. Update the forum key and retry." 3
    fi

    MKV_TITLE=$(echo "$INFO_OUTPUT" | awk -F',' -v idx="DRV:$MKV_INDEX" '$1 == idx {
        n = split($0, a, "\"");
        # fields: ...,"drive name","disc name","device" -> disc name is a[4]
        print a[4]; exit
    }')
    [[ -n "$MKV_TITLE" ]] && RAW_TITLE="$MKV_TITLE"
    echo "Detected disc title: $RAW_TITLE"

    TITLE=$(echo "$RAW_TITLE" | sed -E 's/[._ -]+(SEASON|S|DISC|D|WW|BOOK|VOLUME)[._ -]*[0-9]+([._ -]*(DISC|D)[._ -]*[0-9]+)?[._ -]*$//I')
    TITLE=$(echo "$TITLE" | tr '_' ' ' | tr '[:upper:]' '[:lower:]')
    TITLE=$(echo "$TITLE" | sed -E 's/(^| )([a-z])/\1\U\2/g')
    if [[ -z "$TITLE" ]]; then exit_handler "Error parsing disc metadata!" 3; fi
    echo "General title: $TITLE"

    case "$MEDIA" in
        movie) DEST_REL="Movies/$TITLE" ;;
        show)  DEST_REL="Shows/$TITLE/Season $SEASON" ;;
    esac

    if [[ -d "$PERMANENT/$DEST_REL/Logs" ]]; then
        if compgen -G "$PERMANENT/$DEST_REL/Logs/$RAW_TITLE*.rip.log" > /dev/null; then
            echo "WARNING: Existing rip logs found for '$RAW_TITLE' in $PERMANENT/$DEST_REL/LOGs"
        fi
    fi

    STAGE_DIR="$STAGING/$DEST_REL"
    PERM_DIR="$PERMANENT/$DEST_REL"
    BATCH_DIR="$STAGING/.ripping/$RAW_TITLE.$(date +"%Y-%m-%d_%H-%M-%S")"
    mkdir -p "$BATCH_DIR"
    mkdir -p "$STAGING/.quarantine"
    mkdir -p "$STAGING/.review"
    echo "Batch directory created: $BATCH_DIR"
    echo "Destination: $DEST_REL"

# === Track parsing ===
    declare -A TITLE_NAMES TRACK_NAMES TRACK_SEGMENTS TITLE_DURATIONS TITLE_SIZES TITLE_BYTES SORT_MAP
    mpls_entries=""
    ord_entries=""
    while IFS= read -r line; do
        if [[ "$line" =~ ^TINFO:([0-9]+),16,0,\"(.+)\" ]]; then
            tn="${BASH_REMATCH[1]}"
            src="${BASH_REMATCH[2]}"
            num=$(echo "$src" | grep -oP '[0-9]+(?=\.(mpls|m2ts))')
            [[ -n "$num" ]] && mpls_entries+="$num $tn"$'\n'
        elif [[ "$line" =~ ^TINFO:([0-9]+),2,0,\"(.*)\" ]]; then
            TITLE_NAMES["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"
        elif [[ "$line" =~ ^TINFO:([0-9]+),30,0,\"(.+)\" ]]; then
            TRACK_NAMES["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"
        elif [[ "$line" =~ ^TINFO:([0-9]+),26,0,\"(.+)\" ]]; then
            TRACK_SEGMENTS["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"
        elif [[ "$line" =~ ^TINFO:([0-9]+),9,0,\"(.+)\" ]]; then
            TITLE_DURATIONS["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"
        elif [[ "$line" =~ ^TINFO:([0-9]+),10,0,\"(.+)\" ]]; then
            TITLE_SIZES["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"
        elif [[ "$line" =~ ^TINFO:([0-9]+),11,0,\"(.+)\" ]]; then
            TITLE_BYTES["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"
        elif [[ "$line" =~ ^TINFO:([0-9]+),24,0,\"(.+)\" ]]; then
            ord_entries+="${BASH_REMATCH[2]} ${BASH_REMATCH[1]}"$'\n'
        fi
    done <<< "$INFO_OUTPUT"

    source_entries="${mpls_entries:-$ord_entries}"
    while read -r key val; do
        [[ -n "$key" ]] && SORT_MAP["$key"]="$val"
    done <<< "$source_entries"

    if [[ -z "$source_entries" ]]; then exit_handler "No disc mapping data found in MakeMKV output." 5; fi

    declare -A OUT_MAP
    for key in "${!SORT_MAP[@]}"; do
        track=${SORT_MAP[$key]}
        printf -v line 'Track: %02d | Duration: %s | Size: %s | Title: %s | Segments: %s\n' \
            "$track" \
            "${TITLE_DURATIONS[$track]:-N/A}" \
            "${TITLE_SIZES[$track]:-N/A}" \
            "${TRACK_NAMES[$track]:-N/A}" \
            "${TRACK_SEGMENTS[$track]:-N/A}"
        OUT_MAP[$track]=$line
    done

    TRACK_MAP=""
    for t in $(printf '%s\n' "${!OUT_MAP[@]}" | sort -n); do
        TRACK_MAP+="${OUT_MAP[$t]}"
    done
    printf '%s' "$TRACK_MAP"

    INFO_DEST="$PERMANENT/$DEST_REL/Logs/$RAW_TITLE.info"
    mkdir -p "$(dirname "$INFO_DEST")"
    if [[ -e "$INFO_DEST" ]]; then
        j=1
        while [[ -e "$PERMANENT/$DEST_REL/Logs/$RAW_TITLE(${j}).info" ]]; do
            ((j++))
        done
        INFO_DEST="$PERMANENT/$DEST_REL/Logs/$RAW_TITLE(${j}).info"
    fi
    {
        echo "=== Track Map ==="
        echo "$RAW_TITLE"
        printf '%s' "$TRACK_MAP"
        echo
        echo "=== Raw MakeMKV Info ==="
        echo "$INFO_OUTPUT"
    } > "$INFO_DEST"
    echo "Saved disc info to $INFO_DEST"

    DEST_AVAIL=$(staging_avail)
    echo "Available capacity in staging: $DEST_AVAIL MB"

# === Track selection and size estimation ===
    if $track_select; then
        declare -a selected_tracks
        attempts=0
        while :; do
            echo "Enter track number(s), comma-separated: "
            read -rp "Enter track number(s), comma-separated: " sel
            IFS=',' read -ra raw_input <<< "$sel"

            selected_tracks=()
            for entry in "${raw_input[@]}"; do
                entry="${entry// /}"
                [[ -z "$entry" ]] && continue
                if [[ -z "${OUT_MAP[$entry]+_}" ]]; then
                    echo "Dropped invalid track: $entry"
                    continue
                fi
                bytes="${TITLE_BYTES[$entry]:-0}"
                if (( bytes == 0 )); then
                    echo "Dropped track $entry: no byte-size metadata"
                    continue
                fi
                selected_tracks+=("$entry")
            done

            (( ${#selected_tracks[@]} > 0 )) && break

            ((attempts++))
            (( attempts >= 3 )) && exit_handler "No valid tracks selected after $attempts attempts." 1
            echo "No valid tracks remained after filtering. Try again."
        done

        TOTAL_MB=0
        for t in "${selected_tracks[@]}"; do
            TOTAL_MB=$(( TOTAL_MB + TITLE_BYTES[$t] / 1024 / 1024 ))
        done
        SEL_TRACKS="${selected_tracks[*]}"
        echo "Selected tracks: ${selected_tracks[*]}"
    else
        if [[ "$MEDIA" == "movie" ]]; then
            max_bytes=0
            largest_track=0
            for k in "${!TITLE_BYTES[@]}"; do
                (( TITLE_BYTES[$k] > max_bytes )) && { max_bytes=${TITLE_BYTES[$k]}; largest_track=$k; }
            done
            TOTAL_MB=$(( max_bytes / 1024 / 1024 ))
            SEL_TRACKS="$largest_track"
        else
            TOTAL_MB=0
            SEL_TRACKS="all"
            for t in "${!TITLE_BYTES[@]}"; do
                TOTAL_MB=$(( TOTAL_MB + TITLE_BYTES[$t] / 1024 / 1024 ))
            done
        fi
    fi
    echo "Estimated rip size: $TOTAL_MB MB"

    if sibling_in_starting; then
        echo "Another drive is in Starting; waiting for it to begin ripping ..."
        write_status "Waiting ..."
        waited=0
        while sibling_in_starting; do
            (( waited >= WAIT_MAX_SECS )) && exit_handler "Timed out after ${WAIT_MAX_SECS}s waiting for another drive to leave Starting." 4
            sleep "$WAIT_POLL_SECS"
            waited=$(( waited + WAIT_POLL_SECS ))
        done
        echo "Other drive has progressed; continuing after ${waited}s."
    fi

    NEEDED=$(( TOTAL_MB * 11 / 10 ))
    reserved=$(reserved_by_others)
    effective_avail=$(( DEST_AVAIL - reserved ))
    echo "Reserved by other live rips: $reserved MB | Effective available: $effective_avail MB | Need: $NEEDED MB"

    waited=0
    while (( effective_avail < NEEDED )); do
        largest=$(find "$STAGING/.review" -maxdepth 1 -type f -printf '%s\t%p\n' \
            | sort -rn | head -1 | cut -f2)
        if [[ -n "$largest" ]]; then
            rm -- "$largest"
            echo "Removing $largest to make space."
            waited=0
        elif (( DEST_AVAIL >= NEEDED )); then
            (( waited >= WAIT_MAX_SECS )) && exit_handler "Timed out after ${WAIT_MAX_SECS}s waiting on reserved staging space (need $NEEDED MB, reserved $reserved MB)." 4
            (( waited == 0 )) && echo "Staging blocked only by another rip's reservation ($reserved MB); waiting for it to finish ..."
            write_status "Waiting ..."
            sleep "$WAIT_POLL_SECS"
            waited=$(( waited + WAIT_POLL_SECS ))
        else
            exit_handler "Insufficient staging capacity: need ~$NEEDED MB, raw available $DEST_AVAIL MB (reserved $reserved MB)." 4
        fi
        DEST_AVAIL=$(staging_avail)
        reserved=$(reserved_by_others)
        effective_avail=$(( DEST_AVAIL - reserved ))
    done

# === Rip phase ===
    echo "Starting disc rip ..."

    CURRENT_RIP_MB=0
    if $track_select; then
        curl -sd "[$DRIVE_TAG] Ripping ${#selected_tracks[@]} tracks: $TITLE -> $BATCH_DIR" "$NTFY_URL"
        (
            trap 'kill -TERM "${child:-}" 2>/dev/null; exit 130' TERM INT
            for t in "${selected_tracks[@]}"; do
                echo "[track $t] starting"
                makemkvcon mkv "disc:$MKV_INDEX" "$t" "$BATCH_DIR" &
                child=$!
                wait $child
                rc=$?
                (( rc != 0 )) && exit $rc
            done
        ) &
        MKV_PID=$!
        echo "Driver PID: $MKV_PID"
    else
        curl -sd "[$DRIVE_TAG] Ripping: $TITLE -> $BATCH_DIR" "$NTFY_URL"
        makemkvcon mkv "disc:$MKV_INDEX" "$SEL_TRACKS" "$BATCH_DIR" &
        MKV_PID=$!
        echo "MakeMKV PID: $MKV_PID"
    fi

    while kill -0 $MKV_PID 2>/dev/null; do
        CURRENT_RIP_MB=$(safe_du "$BATCH_DIR")
        write_status "Ripping ..."
        sleep 1
    done
    wait $MKV_PID
    mkv_exit=$?

    echo "Rip exited with EXIT_CODE: $mkv_exit"
    # (( mkv_exit != 0 )) && exit_handler "MakeMKV rip failed with exit EXIT_CODE $mkv_exit" 5

    echo "Final output from disc:"
    tree -htFDQ --du "$BATCH_DIR"

# === Sort phase ===
    sorted_files=()
    sorted_titles=()

    if (( ${#SORT_MAP[@]} > 0 )); then
        echo "Attempting source-order mapping ..."
        for src_num in $(printf '%s\n' "${!SORT_MAP[@]}" | sort -n); do
            title_num="${SORT_MAP[$src_num]}"
            printf -v padded "t%02d" "$title_num"
            f="$BATCH_DIR/title_${padded}.mkv"
            if [[ ! -f "$f" ]]; then
                fn=$(echo "$INFO_OUTPUT" | grep "^TINFO:${title_num},27,0," | sed 's/.*"\(.*\)"/\1/')
                f="$BATCH_DIR/$fn"
            fi
            if [[ -f "$f" ]]; then
                sorted_files+=("$f")
                sorted_titles+=("$title_num")
            fi
        done
        echo "Source-order matched ${#sorted_files[@]} of ${#SORT_MAP[@]} entries."
    fi

    actual_mkv_count=$(find "$BATCH_DIR" -maxdepth 1 -type f -name '*.mkv' | wc -l)
    if [[ ${#sorted_files[@]} -ne $actual_mkv_count ]]; then
        echo "Source-order matched ${#sorted_files[@]} but found $actual_mkv_count files. Discarding partial match, falling back ..."
        sorted_files=()
        sorted_titles=()
    fi

    if [[ ${#sorted_files[@]} -eq 0 ]]; then
        echo "Falling back to MakeMKV rip order ..."
        fallback_idx=0
        while IFS= read -r -d '' f; do
            bn=$(basename "$f" .mkv)
            tn_raw=$(echo "$bn" | grep -oP '_t\K[0-9]+' | head -n1)
            if [[ -n "$tn_raw" ]]; then
                tn=$((10#$tn_raw))
            else
                tn=$fallback_idx
            fi
            sorted_files+=("$f")
            sorted_titles+=("$tn")
            ((fallback_idx++))
        done < <(find "$BATCH_DIR" -maxdepth 1 -type f -name '*.mkv' -print0 | sort -z)
        echo "Rip-order fallback collected ${#sorted_files[@]} files."
    fi

    if [[ ${#sorted_files[@]} -eq 0 ]]; then exit_handler "No mkv files found in batch directory." 6; fi

    if [[ "$MEDIA" == "show" ]]; then
        mkdir -p "$STAGE_DIR/Extras"

        all_sizes=()
        for f in "${sorted_files[@]}"; do
            all_sizes+=("$(( $(stat -c%s "$f") / 1024 / 1024 ))")
        done
        mapfile -t sorted_sz < <(printf '%s\n' "${all_sizes[@]}" | sort -rn)
        anchor_size="${sorted_sz[1]:-${sorted_sz[0]}}"
        extras_thresh=$(( anchor_size * 70 / 100 ))
        trash_thresh=$(( anchor_size * 130 / 100 ))
        echo "Anchor: ${anchor_size}MB | Extras: <${extras_thresh}MB | Review: >${trash_thresh}MB"

        highest=0
        for dir in "$PERM_DIR" "$STAGE_DIR"; do
            [[ -d "$dir" ]] || continue
            for existing in "$dir"/*.mkv; do
                [[ -e "$existing" ]] || continue
                num=$(basename "$existing" | grep -oP 'S[0-9]+E\K[0-9]+' | sed 's/^0*//')
                [[ -n "$num" && "$num" -gt "$highest" ]] && highest=$num
            done
        done
        offset=$highest
        echo "Offset: $offset"

        ep=1
        for idx in "${!sorted_files[@]}"; do
            f="${sorted_files[$idx]}"
            tn="${sorted_titles[$idx]}"
            name="${TITLE_NAMES[$tn]:-}"
            dur="${TITLE_DURATIONS[$tn]:-??:??:??}"

            f_size=$(( $(stat -c%s "$f") / 1024 / 1024 ))
            i=$(( ep + offset ))

            if (( f_size > trash_thresh )) || (( f_size < extras_thresh )); then
                mkdir -p "$STAGING/.review"
                dest="$STAGING/.review/$RAW_TITLE.$(basename "$f")"
                echo "title $tn: $(basename "$f") '$name' ($dur) -> $dest (review, not tracked)"
                mv "$f" "$dest"
                continue
            else
                dest="$STAGE_DIR/$TITLE S${SEASON}E${i}.mkv"
                if [[ -e "$dest" ]]; then
                    j=1
                    while [[ -e "$STAGE_DIR/$TITLE S${SEASON}E${i}_${j}.mkv" ]]; do
                        ((j++))
                    done
                    dest="$STAGE_DIR/$TITLE S${SEASON}E${i}_${j}.mkv"
                fi
                ((ep++))
            fi
            echo "title $tn: $(basename "$f") '$name' ($dur) -> $dest"
            mv "$f" "$dest"
            STAGED_PATHS+=("$dest")
        done
    else
        mkdir -p "$STAGE_DIR/Extras"
        largest=""
        largest_size=0
        for f in "$BATCH_DIR"/*.mkv; do
            [[ -f "$f" ]] || continue
            s=$(stat -c%s "$f")
            if (( s > largest_size )); then
                largest="$f"
                largest_size=$s
            fi
        done
        for f in "$BATCH_DIR"/*.mkv; do
            [[ -f "$f" ]] || continue
            if [[ "$f" == "$largest" ]]; then
                dest="$STAGE_DIR/$(basename "$f")"
                echo "$(basename "$f") -> $STAGE_DIR/"
                mv "$f" "$STAGE_DIR/"
            else
                dest="$STAGE_DIR/Extras/$(basename "$f")"
                echo "$(basename "$f") -> $STAGE_DIR/Extras/"
                mv "$f" "$STAGE_DIR/Extras/"
            fi
            STAGED_PATHS+=("$dest")
        done
    fi

    rm -rf "$BATCH_DIR"
    echo "Removed empty batch dir: $BATCH_DIR."

    if $eject_flag && drive_state; then
        echo "Ejecting Disc."
        eject "$DEVICE"
        eject_flag=false
    fi

# === Scan phase===
    allowed_mimes=(
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
    declare -A scanners
    
    write_status "Scanning ..."

    while IFS= read -r -d '' file; do
        echo "Processing: $(basename "$file")"
        {
            mime="$(file --mime-type -b "$file")"
            mime_ok=false
            for allowed in "${allowed_mimes[@]}"; do
                [[ "$mime" == "$allowed" ]] && { mime_ok=true; break; }
            done
            if [[ "$mime_ok" == false ]]; then
                echo "FAIL: Unexpected MIME type '$mime' for $file; $file  -> $STAGING/.quarantine"
                mv "$file" "$STAGING/.quarantine/"
                exit 1
            fi
            clamdscan --quiet --fdpass "$file"
            rc=$?
            case $rc in
                0) ;;
                1)
                    echo "FAIL: ClamAV flagged $file -> $STAGING/.quarantine"
                    mv "$file" "$STAGING/.quarantine/"
                    exit 1
                    ;;
                *)
                    echo "ERROR: clamdscan failed to scan $file (rc=$rc)"
                    exit 2
                    ;;
            esac
        } &
        scanners[$!]="$file"
    done < <(find "$STAGE_DIR" -type f -print0)

    while (( ${#scanners[@]} > 0 )); do
        for pid in "${!scanners[@]}"; do
            if ! kill -0 "$pid" 2>/dev/null; then
                wait "$pid"
                status=$?
                if ((status == 0)); then
                    echo "scan done: ${scanners[$pid]}"
                else
                    echo "FAIL: exit (${status}) ${scanners[$pid]}; ${scanners[$pid]} -> $STAGING/.quarantine"
                fi
                unset 'scanners[$pid]'
            fi
        done
        write_status "Scanning ..."
        sleep 1
    done

    echo "File scans complete!" 

# === Promote staged files to permanent ===
    echo "Promoting staged files to $PERM_DIR ..."
    mkdir -p "$PERM_DIR"

    exec 201>"/var/lock/movie-ripper.promote.lock"
    flock 201

    STAGE_START_SIZE=$(safe_du "$STAGE_DIR")
    CURRENT_MV_MB=0
    write_status "Moving ..."

    rsync -a --inplace --remove-source-files --info=progress2 --outbuf=N \
          "$STAGE_DIR/" "$PERM_DIR/" \
        | while IFS= read -r -d $'\r' line; do
              [[ "$line" == *%* ]] || continue
              bytes=$(grep -oE '[0-9,]+' <<<"$line" | head -1 | tr -d ',')
              [[ "$bytes" =~ ^[0-9]+$ ]] || continue
              CURRENT_MV_MB=$(( bytes / 1024 / 1024 ))
              write_status "Moving ..."
          done
    rc=${PIPESTATUS[0]}

    find "$STAGE_DIR" -type d -empty -delete 2>/dev/null || true

    if (( rc != 0 )); then
        echo "WARN: rsync exited $rc; unverified files left in $STAGE_DIR"
    fi

    CURRENT_MV_MB=$STAGE_START_SIZE
    write_status "Moving ..."
    echo "Promotion complete."

exit_handler "Pipeline complete for $DEST_REL." 0