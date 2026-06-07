#!/usr/bin/env bash
# run.sh: build the toolchain, run every Novel benchmark, and tabulate results.
#
# Each benchmark prints one line to stdout: "<name>\t<ms>\t<checksum>". The
# benchmark times its own hot region with std/time, so transpile and LuaJIT
# startup cost stay out of the measurement. We run each benchmark REPS times
# and keep the minimum (least noise) time, then write a timestamped TSV under
# results/ for later comparison with compare.sh.
#
# Usage:
#   benchmark/run.sh [REPS]      # default REPS=3
#
# Run from anywhere; paths are resolved relative to this script.

set -euo pipefail

REPS="${1:-3}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"

# Check if we are running on Windows.
EXE=""
if [[ "${OSTYPE:-}" == "msys" || "${OSTYPE:-}" == "cygwin" || -n "${WINDIR:-}" ]]; then
    EXE=".exe"
fi

novel="$repo_root/bin/novel$EXE"
results_dir="$script_dir/results"

# Build the toolchain so we measure current codegen, not a stale binary.
# Build directly with `go build` (not make) so the harness is self-contained.
echo "building toolchain..."
( cd "$repo_root" && go build -o "$novel" ./cmd/novel )

mkdir -p "$results_dir"
stamp="$(date +%Y%m%d-%H%M%S)"
outfile="$results_dir/$stamp.tsv"

printf "benchmark\tmin_ms\tchecksum\n" >"$outfile"
printf "%-22s %12s   %s\n" "benchmark" "min_ms" "checksum"
printf -- "------------------------------------------------------------\n"

# Stable run order so tables line up across runs.
for path in "$script_dir"/*.nv; do
    name="$(basename "$path" .nv)"

    best_ms=""
    checksum=""
    for _ in $(seq 1 "$REPS"); do
        line="$("$novel" run "$path")"
        # Expected: name<TAB>ms<TAB>checksum
        ms="$(printf '%s' "$line" | cut -f2)"
        checksum="$(printf '%s' "$line" | cut -f3)"
        if [ -z "$best_ms" ] || awk "BEGIN{exit !($ms < $best_ms)}"; then
            best_ms="$ms"
        fi
    done

    printf "%-22s %12.3f   %s\n" "$name" "$best_ms" "$checksum"
    printf "%s\t%s\t%s\n" "$name" "$best_ms" "$checksum" >>"$outfile"
done

# Clean up any .lua files a stray `novel build` may have left behind.
rm -f "$script_dir"/*.lua 2>/dev/null || true

echo
echo "wrote $outfile"
