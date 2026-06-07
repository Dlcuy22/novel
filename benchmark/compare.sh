#!/usr/bin/env bash
# compare.sh: show the per-benchmark speed change between two result files.
#
# Both files are TSVs written by run.sh (benchmark<TAB>min_ms<TAB>checksum).
# For each benchmark present in both, prints old/new milliseconds and the
# percent change (negative means the new run is faster). Also flags any
# benchmark whose checksum changed, which means an optimization altered output
# and the timing comparison is not valid.
#
# Usage:
#   benchmark/compare.sh OLD.tsv NEW.tsv

set -euo pipefail

if [ "$#" -ne 2 ]; then
    echo "usage: benchmark/compare.sh OLD.tsv NEW.tsv" >&2
    exit 2
fi

old="$1"
new="$2"

for f in "$old" "$new"; do
    if [ ! -f "$f" ]; then
        echo "no such file: $f" >&2
        exit 1
    fi
done

awk -F'\t' '
    FNR == NR {
        if (FNR > 1) { old_ms[$1] = $2; old_sum[$1] = $3 }
        next
    }
    FNR == 1 {
        printf "%-22s %12s %12s %10s\n", "benchmark", "old_ms", "new_ms", "change"
        printf "%s\n", "----------------------------------------------------------------"
        next
    }
    {
        name = $1; new_ms = $2; new_sum = $3
        if (!(name in old_ms)) next
        delta = (new_ms - old_ms[name]) / old_ms[name] * 100.0
        flag = ""
        if (new_sum != old_sum[name]) flag = "  CHECKSUM CHANGED"
        printf "%-22s %12.3f %12.3f %+9.1f%%%s\n", name, old_ms[name], new_ms, delta, flag
    }
' "$old" "$new"
