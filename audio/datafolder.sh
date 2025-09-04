#!/usr/bin/env bash
#

DATADIR=""
if [ ! -d "$1" ]; then
    # first param is a folder like "/home/ern/tes3/mods/ModdingResources/TamrielData/00 Data Files/"
    echo "Data directory '$1' does not exist."
fi
DATADIR="$1"

# this gets a list of files, but it's not guaranteed that the top level is "vo".
#find "$DATADIR" -type f | grep -i "/vo/" | sed 's!^.*sound/!!ig'


find "$DATADIR" -type f \
  | grep -i "/vo/" \
  | sed 's!^.*sound/!!ig' \
  | while read -r path; do
      # --- First column: the path as-is
      col1="$path"

      # --- Second column: second-to-last token of basename (split by "_")
      base="$(basename "$path" .mp3)"
      IFS="_" read -ra parts <<< "$base"
      if (( ${#parts[@]} >= 2 )); then
        col2="${parts[-2]}"
      else
        col2="$base"   # fallback if no underscore
      fi

      # --- Third column: second-to-last folder
      folder=$(dirname "$path")
      second_last=$(basename "$(dirname "$folder")")
      col3="$second_last"

      # --- Fourth column: last folder
      last=$(basename "$folder")
      col4="$last"

      # --- Output row (quoted)
      echo "\"$col1\",\"?$col2\",\"$col3\",\"$col4\""
    done
