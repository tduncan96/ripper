#!/usr/bin/env bash
set -euo pipefail

LOG_FILE=$(mktemp -t media-librarian-XXXXXX.log)
exec > >(tee -a "$LOG_FILE") 2>&1
trap 'rm -f "$LOG_FILE"' EXIT

log() { printf '[%s] %s\n' "$(date -Iseconds)" "$*"; }


JELLYFIN_URL="$1"
JELLYFIN_API_KEY="$2"
BOOKSTACK_URL="$3"
BOOKSTACK_PAGE_ID="$4"
BOOKSTACK_TOKEN_ID="$5"
BOOKSTACK_API_KEY="$6"
OUT_DIR="$7"

mkdir -p "$OUT_DIR"

curl_jf() {
  curl -fsS -H "X-Emby-Token: $JELLYFIN_API_KEY" "$JELLYFIN_URL"
}

log "Fetching movies"
curl_jf "/Items?Recursive=true&IncludeItemTypes=Movie&Fields=Path,Genres,ProductionYear,MediaSources,RunTimeTicks" \
  | jq '[.Items[] | {name: .Name, year: .ProductionYear, path: .Path, runtime_min: ((.RunTimeTicks // 0) / 600000000 | floor), codec: .MediaSources[0].MediaStreams[0].Codec, height: .MediaSources[0].MediaStreams[0].Height, size_gb: ((.MediaSources[0].Size // 0) / 1073741824 * 100 | floor / 100)}]' \
  > "$OUT_DIR/movies.json"
log "Wrote $(jq 'length' "$OUT_DIR/movies.json") movies"

log "Fetching shows"
curl_jf "/Items?Recursive=true&IncludeItemTypes=Series&Fields=Path,Genres,ProductionYear&SortBy=SortName" \
  | jq -c '.Items[]' \
  | while read -r series; do
      sid=$(jq -r '.Id' <<<"$series")
      seasons=$(curl_jf "/Shows/$sid/Seasons" \
        | jq -c '[.Items[] | {season_id: .Id, season_name: .Name, season_number: .IndexNumber}]')

      seasons_with_counts=$(jq -c '.[]' <<<"$seasons" \
        | while read -r season; do
            season_id=$(jq -r '.season_id' <<<"$season")
            eps=$(curl_jf "/Shows/$sid/Episodes?seasonId=$season_id" | jq '.TotalRecordCount')
            jq --argjson eps "$eps" '{name: .season_name, number: .season_number, episode_count: $eps}' <<<"$season"
          done | jq -s '.')

      total=$(jq '[.[].episode_count] | add // 0' <<<"$seasons_with_counts")

      jq --argjson seasons "$seasons_with_counts" --argjson total "$total" \
        '{name: .Name, year: .ProductionYear, path: .Path, episode_count: $total, seasons: $seasons}' <<<"$series"
    done | jq -s '.' > "$OUT_DIR/shows.json"
log "Wrote $(jq 'length' "$OUT_DIR/shows.json") shows"

log "Fetching albums"
curl_jf "/Items?Recursive=true&IncludeItemTypes=MusicAlbum&Fields=Path,Genres,ProductionYear,AlbumArtist,ChildCount" \
  | jq '[.Items[] | {artist: .AlbumArtist, album: .Name, year: .ProductionYear, path: .Path, track_count: .ChildCount}]' \
  > "$OUT_DIR/albums.json"
log "Wrote $(jq 'length' "$OUT_DIR/albums.json") albums"

log "Generating catalog markdown"
{
  echo "## Summary"
  printf -- "- Movies: %s\n" "$(jq 'length' "$OUT_DIR/movies.json")"
  printf -- "- Shows: %s\n"  "$(jq 'length' "$OUT_DIR/shows.json")"
  printf -- "- Albums: %s\n" "$(jq 'length' "$OUT_DIR/albums.json")"
  echo
  echo "### Movies"
  jq -r '.[] | "- \(.name) (\(.year // "?")) — \(.height // "?")p \(.codec // "?") — \(.size_gb)GB"' "$OUT_DIR/movies.json" | sort
  echo
  echo "### Shows"
  jq -r '
    .[] |
    "- \(.name) (\(.year // "?")) — \(.episode_count) eps",
    (.seasons[] | "    - \(.name): \(.episode_count) eps")
  ' "$OUT_DIR/shows.json"
  echo
  echo "### Albums"
  jq -r '.[] | "- \(.artist // "Unknown") — \(.album) (\(.year // "?")) — \(.track_count) tracks"' "$OUT_DIR/albums.json" | sort
} > "$OUT_DIR/catalog.md"


curl_bs() {
  curl -fsS -H "Authorization: Token ${BOOKSTACK_TOKEN_ID}:${BOOKSTACK_API_KEY}" "$@"
}

if jq -Rs '{markdown: ., name: "Media Catalog"}' < "$OUT_DIR/catalog.md" \
  | curl_bs -X PUT \
      -H "Content-Type: application/json" \
      --data-binary @- \
      "$BOOKSTACK_URL/api/pages/$BOOKSTACK_PAGE_ID" \
  > /dev/null; then
    log "Updated BookStack page $BOOKSTACK_PAGE_ID"
else
    log "BookStack update FAILED"
    exit 1
fi

log "Done"

log "Cleaning up old log attachments"
OLD_IDS=$(curl_bs "$BOOKSTACK_URL/api/attachments" \
  | jq --argjson page "$BOOKSTACK_PAGE_ID" -r \
      '.data[] | select(.uploaded_to == $page and .name == "catalog-run.log") | .id')

for id in $OLD_IDS; do
  log "Deleting old attachment $id"
  curl_bs -X DELETE "$BOOKSTACK_URL/api/attachments/$id" > /dev/null
done

log "Uploading fresh log attachment"
curl_bs -X POST \
  -F "uploaded_to=$BOOKSTACK_PAGE_ID" \
  -F "name=catalog-run.log" \
  -F "file=@$LOG_FILE" \
  "$BOOKSTACK_URL/api/attachments" \
  > /dev/null

log "Attachment uploaded"