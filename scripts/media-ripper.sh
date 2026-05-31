#!/bin/bash
set -uo pipefail

# === Helper functions ===
    exit_handler() {
        local msg="$1"
        local code="${2:-255}"
        local phase

        if [[ -n "${tail_pid:-}" ]]; then
            kill "$tail_pid" 2>/dev/null
            wait "$tail_pid" 2>/dev/null
        fi
        sync

        # Error codes:
        # 1 -> Pre-flight error
        # 2 -> No disc in drive error
        # 3 -> Disc error
        # 4 -> Capacity error
        # 5 -> MakeMKV error
        # 6 -> File indexing error

        if (( code == 0 )); then
            phase="Complete"
        else
            phase="Failed [$code]"
        fi

        write_status "$phase"

        echo -e "[$code] $msg"
        curl -sd "[$DRIVE_TAG] [$code] $msg" NTFY_URL

        if (( code == 1 )); then eject_flag=false; fi

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

        local log_dest
        if [[ -n "$DEST_REL" ]]; then
            log_dest="$PERMANENT/$DEST_REL/logs/$RAW_TITLE.rip.log"
            mkdir -p "$(dirname "$log_dest")"
            if [[ -e "$log_dest" ]]; then
                local j=1
                while [[ -e "$PERMANENT/$DEST_REL/logs/$RAW_TITLE(${j}).rip.log" ]]; do
                    ((j++))
                done
                log_dest="$PERMANENT/$DEST_REL/logs/$RAW_TITLE(${j}).rip.log"
            fi
        else
            local ts fallback_dir
            ts=$(date +"%Y-%m-%d_%H-%M-%S")
            fallback_dir="${PERMANENT:-/mnt/14tb_sata_1/media}/failure_logs"
            mkdir -p "$fallback_dir"
            log_dest="$fallback_dir/$ts.rip.log"
        fi
        echo "Exporting log to $log_dest ..."
        cp "$log" "$log_dest"
        rm -rf "$log"

        echo "merciful bliss ..."
        exit "$code"
    }

    # shellcheck disable=SC2329
    cleanup_on_signal() {
        trap '' INT TERM
        echo ""
        echo "Signal received, cleaning up child processes ..."
        [[ -n "${MKV_PID:-}" ]] && kill -TERM "$MKV_PID" 2>/dev/null
        [[ -n "${tail_pid:-}" ]] && kill -TERM "$tail_pid" 2>/dev/null
        sleep 2
        [[ -n "${MKV_PID:-}" ]] && kill -KILL "$MKV_PID" 2>/dev/null
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

    staged_paths_remain() {
        local p
        for p in "${STAGED_PATHS[@]}"; do
            [[ -e "$p" ]] && return 0
        done
        return 1
    }

    staged_paths_size() {
        local total=0 p sz
        for p in "${STAGED_PATHS[@]}"; do
            [[ -e "$p" ]] || continue
            sz=$(safe_du "$p" 0)
            total=$(( total + sz ))
        done
        echo "$total"
    }

    write_status() {
        local phase="$1"
        local now elapsed_seconds full_dest

        [[ -z "${STATUS:-}" ]] && return 0

        now=$(date +%s)
        elapsed_seconds=$((now - START))

        if [[ "$phase" == "Ripping" ]]; then
            full_dest="$BATCH_DIR"
        else
            full_dest="$PERM_DIR"
        fi

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
            --arg rip_log "$(cat $log)" \
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
                updated: $updated,
                updated_epoch: $updated_epoch,
                rip_log: $rip_log
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
            case "$phase" in
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
        local pattern="${STATUS%.*}.*.${STATUS##*.}"
        for f in $pattern; do
            [[ -e "$f" ]] || continue
            [[ "$f" == "$STATUS" ]] && continue
            data=$(jq -r '[.phase // "", .updated_epoch // 0] | @tsv' "$f" 2>/dev/null) || continue
            IFS=$'\t' read -r phase upd <<< "$data"
            [[ "$phase" == "Starting" ]] || continue
            [[ "$upd" =~ ^[0-9]+$ ]] || upd=0
            (( now - upd > RESERVE_STALE_SECS )) && continue   # stale: don't block on it
            return 0
        done
        return 1
    }

# === Initialization ===
    START=$(date +%s)
    
    STAGING="${1:-}"
    PERMANENT="${2:-}"
    STATUS_TMP="${3:-}"
    LOG_TMP="${4:-}"
    NTFY_URL="${5:-}"
    DRIVE_NUM="${6:-}"
    MEDIA="${7:-}"
    SEASON="${8:-}"

    DEVICE="/dev/sr$DRIVE_NUM"
    DRIVE_TAG="$(basename "$DEVICE")".
    STATUS="${STATUS_TMP/\*/$DRIVE_TAG}"
    LOG="${LOG_TMP/\*/$DRIVE_TAG}"
    RIPPING="$STAGING/.ripping"

    WAIT_POLL_SECS=10
    WAIT_MAX_SECS=5400
    RESERVE_STALE_SECS=120

    DEST_REL=""
    RAW_TITLE=""
    TITLE=""
    BATCH_DIR=""
    PERM_DIR=""
    MKV_INDEX=""
    SEL_TRACKS=""

    declare -a STAGED_PATHS=()

    eject_flag=false
    track_select=false

    exec > >(ts '%Y-%m-%d %H:%M:%S' | tee -a "$LOG") 2>&1
    echo "Run PID: $$"
    echo "Command Received: $0 $*"
    echo "Start: $(date)"

    curl -sd "Rip Request Received" NTFY_URL


# === Arg parsing and preflight ===
    deps=(makemkvcon jq inotifywait)
    for dep in "${deps[@]}"; do
        if ! command -v "dep"; then
            exit_handler "Missing required dependency: $dep" 1
        fi
    done
    
    while getopts "Et" opt; do
        case $opt in
            E) eject_flag=true ;;
            t) track_select=true ;;
            *) exit_handler "[?] Invalid option: -$OPTARG" 1 ;;
        esac
    done
    shift $((OPTIND - 1))

    if [[ -z "$DRIVE_NUM" ]]; then
        echo "Missing drive number. $USAGE"; exit 1
    fi
    if [[ ! "$DRIVE_NUM" =~ ^[0-9]+$ ]]; then
        echo "Drive number must be a non-negative integer (got '$DRIVE_NUM'). $USAGE"; exit 1
    fi

    echo "Drive: $DEVICE (tag: $DRIVE_TAG)"
    echo "Status file: $STATUS"

    trap cleanup_on_signal INT TERM

    write_status "Starting"

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

    if ! systemctl is-active --quiet staging-watcher; then exit_handler "Service staging-watcher is not running." 1; fi

    if ! smart_result=$(sudo -n /root/scripts/smart_check.sh "$STAGING" 2>&1); then
        echo "WARNING: $smart_result"
        curl -sd "[$DRIVE_TAG] WARN: SMART check on staging - $smart_result" NTFY_URL
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
    if ! udevadm info --query=property --name="$DEVICE" | grep -q '^ID_FS_MEDIA='; then
        exit_handler "Disc unreadable; no recognizable filesystem. Exiting ..." 3
    fi

# === MakeMKV index resolution ===
    echo "Resolving MakeMKV disc index for $DEVICE ..."
    write_status "Indexing ..."
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

# === Disc info and title parsing ===
    echo "Getting disc info ..."
    write_status "Getting Disc Info ..."
    INFO_OUTPUT=$(makemkvcon -r info "disc:$MKV_INDEX")
    RAW_TITLE=$(echo "$INFO_OUTPUT" | awk -F',' -v idx="DRV:$MKV_INDEX" '$1 == idx {
        n = split($0, a, "\"");
        # fields: ...,"drive name","disc name","device" -> disc name is a[4]
        print a[4]; exit
    }')
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

    if [[ -d "$PERMANENT/$DEST_REL/logs" ]]; then
        if compgen -G "$PERMANENT/$DEST_REL/logs/$RAW_TITLE*.rip.log" > /dev/null; then
            echo "WARNING: Existing rip logs found for '$RAW_TITLE' in $PERMANENT/$DEST_REL/LOGs"
        fi
    fi

# === Directory Setting ===
    STAGE_DIR="$STAGING/$DEST_REL"
    PERM_DIR="$PERMANENT/$DEST_REL"
    BATCH_DIR="$RIPPING/$RAW_TITLE.$(date +"%Y-%m-%d_%H-%M-%S")"
    mkdir -p "$BATCH_DIR"
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

    for t in $(printf '%s\n' "${!OUT_MAP[@]}" | sort -n); do
        printf '%s' "${OUT_MAP[$t]}"
    done

    INFO_DEST="$PERMANENT/$DEST_REL/logs/$RAW_TITLE.info"
    mkdir -p "$(dirname "$INFO_DEST")"
    if [[ -e "$INFO_DEST" ]]; then
        j=1
        while [[ -e "$PERMANENT/$DEST_REL/logs/$RAW_TITLE(${j}).info" ]]; do
            ((j++))
        done
        INFO_DEST="$PERMANENT/$DEST_REL/logs/$RAW_TITLE(${j}).info"
    fi
    {
        echo "=== Track Map ==="
        echo "$RAW_TITLE"
        for t in $(printf '%s\n' "${!OUT_MAP[@]}" | sort -n); do
            printf '%s' "${OUT_MAP[$t]}"
        done
        echo
        echo "=== Raw MakeMKV Info ==="
        echo "$INFO_OUTPUT"
    } > "$INFO_DEST"
    echo "Saved disc info to $INFO_DEST"

    DEST_AVAIL=$(df --output=avail -BM "$STAGING" | tail -n1 | tr -d ' M')
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
            max_bytes=
            largest_track=
            for k in "${!TITLE_BYTES[@]}"; do
                v=${TITLE_BYTES[$k]}
                if [[ -z "$max" || "$v" -gt "$max" ]]
                    max_bytes=$v
                    largest_track=$k
                fi
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
        write_status "Waiting"
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
            write_status "Waiting"
            sleep "$WAIT_POLL_SECS"
            waited=$(( waited + WAIT_POLL_SECS ))
        else
            exit_handler "Insufficient staging capacity: need ~$NEEDED MB, raw available $DEST_AVAIL MB (reserved $reserved MB)." 4
        fi
        DEST_AVAIL=$(df --output=avail -BM "$STAGING" | tail -n1 | tr -dc '0-9')
        reserved=$(reserved_by_others)
        effective_avail=$(( DEST_AVAIL - reserved ))
    done

# === Rip phase ===
    echo "Starting disc rip ..."

    CURRENT_RIP_MB=0
    if $track_select; then
        curl -sd "[$DRIVE_TAG] Ripping ${#selected_tracks[@]} tracks: $TITLE -> $BATCH_DIR" NTFY_URL
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
        curl -sd "[$DRIVE_TAG] Ripping: $TITLE -> $BATCH_DIR" NTFY_URL
        makemkvcon mkv "disc:$MKV_INDEX" "$SEL_TRACKS" "$BATCH_DIR" &
        MKV_PID=$!
        echo "MakeMKV PID: $MKV_PID"
    fi

    while kill -0 $MKV_PID 2>/dev/null; do
        CURRENT_RIP_MB=$(safe_du "$BATCH_DIR")
        write_status "Ripping"
        sleep 1
    done
    wait $MKV_PID
    mkv_exit=$?

    echo "Rip exited with code: $mkv_exit"
    # (( mkv_exit != 0 )) && exit_handler "MakeMKV rip failed with exit code $mkv_exit" 5

    echo "Final output from disc:"
    tree -htFDQ --du "$BATCH_DIR"

# === Tail check.log into rip.log for the move phase ===
    stdbuf -oL tail -n 0 -F "/var/log/staging-watcher/check.log" >> "$LOG" &
    tail_pid=$!
    trap 'kill "${tail_pid:-}" 2>/dev/null' EXIT
    echo "Check Tail PID: $tail_pid"

    sleep 5

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
            all_sizes+=("$(stat -c%s "$f" | awk '{printf "%d", $1/1024/1024}')")
        done
        mapfile -t sorted_sz < <(printf '%s\n' "${all_sizes[@]}" | sort -rn)
        anchor_size="${sorted_sz[1]:-${sorted_sz[0]}}"
        extras_thresh=$(( anchor_size * 75 / 100 ))
        trash_thresh=$(( anchor_size * 125 / 100 ))
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

            f_size=$(stat -c%s "$f" | awk '{printf "%d", $1/1024/1024}')
            i=$(( ep + offset ))

            if (( f_size > trash_thresh )); then
                mkdir -p "$STAGING/.review"
                dest="$STAGING/.review/$RAW_TITLE.$(basename "$f")"
                echo "title $tn: $(basename "$f") '$name' ($dur) -> $dest (review, not tracked)"
                mv "$f" "$dest"
                continue
            elif (( f_size <= extras_thresh )); then
                j=1
                dest="$STAGE_DIR/Extras/$TITLE S$SEASON Extra ${j}.mkv"
                while [[ -e "$dest" || -e "$PERM_DIR/Extras/$TITLE S$SEASON Extra ${j}.mkv" ]]; do
                    ((j++))
                    dest="$STAGE_DIR/Extras/$TITLE S$SEASON Extra ${j}.mkv"
                done
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

# === Drain wait ===
    echo "Rip Complete! Files staged to: $STAGE_DIR. Waiting for staging pipeline to complete ..."

    CURRENT_MV_MB=0
    STAGE_START_SIZE=$(staged_paths_size)
    echo "Initial drain pool: ${STAGE_START_SIZE} MB"

    while staged_paths_remain; do
        stage_current=$(staged_paths_size)
        CURRENT_MV_MB=$(( STAGE_START_SIZE - stage_current ))
        (( CURRENT_MV_MB < 0 )) && CURRENT_MV_MB=0
        (( CURRENT_MV_MB > TOTAL_MB )) && CURRENT_MV_MB=$TOTAL_MB
        write_status "Moving"
        sleep 1
    done

    echo "File moves complete."

    stage_current=$(staged_paths_size)
    CURRENT_MV_MB=$(( STAGE_START_SIZE - stage_current ))
    (( CURRENT_MV_MB < 0 )) && CURRENT_MV_MB=0
    (( CURRENT_MV_MB > TOTAL_MB )) && CURRENT_MV_MB=$TOTAL_MB

exit_handler "Pipeline complete for $DEST_REL." 0