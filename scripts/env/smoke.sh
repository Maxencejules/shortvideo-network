#!/usr/bin/env bash
# Database query, AMQP round trip, and a small FFmpeg operation.
# Services must already be running. This command does not start them.
set -euo pipefail
source "$(dirname "$0")/common.sh"
SVN_LOG_PREFIX="env-smoke"

if [[ $# -ne 0 ]]; then
  die "usage: smoke.sh  (workspace from SVN_WORKSPACE, default dev)" 2
fi

ws="$(svn_workspace_path)" || exit $?
cred="$(svn_credential_file "${ws}")"
svn_load_credentials "${cred}"

if ! svn_postgres_running "${ws}"; then
  die "postgres is not running. Run scripts/env/start.sh" 2
fi
if ! svn_rabbit_running "${ws}"; then
  die "rabbitmq is not running. Run scripts/env/start.sh" 2
fi
command -v ffmpeg >/dev/null || die "ffmpeg is not installed. Run scripts/env/install.sh" 2
command -v amqp-publish >/dev/null || die "amqp-publish is not installed. Run scripts/env/install.sh" 2

say "postgres query"
svn_psql "${ws}" -c 'SELECT 1 AS env_smoke_ok;'

url="$(svn_amqp_url)"
body="goal04-roundtrip"
say "amqp round trip"
amqp-declare-queue --url="${url}" --queue=svn-smoke --durable >/dev/null
amqp-publish --url="${url}" --routing-key=svn-smoke --body="${body}"
got="$(timeout 20 amqp-consume --url="${url}" --queue=svn-smoke --count=1 cat)"
got="${got//$'\n'/}"
if [[ "${got}" != "${body}" ]]; then
  die "queue round trip returned [${got}]"
fi
say "amqp round trip matched"

mkdir -p "${ws}/ffmpeg"
say "ffmpeg lavfi"
ffmpeg -hide_banner -loglevel error -f lavfi -i "color=c=black:s=16x16:d=0.1" -frames:v 1 -f null -
ffmpeg -hide_banner -loglevel error -y -f lavfi -i "sine=frequency=440:duration=0.2" -f wav "${ws}/ffmpeg/tone.wav"
[[ -s "${ws}/ffmpeg/tone.wav" ]] || die "ffmpeg did not write a wav file"
ffprobe -hide_banner -loglevel error -show_entries format=format_name -of csv=p=0 "${ws}/ffmpeg/tone.wav" \
  | grep -q wav || die "ffprobe did not report wav"
say "ffmpeg wrote ${ws}/ffmpeg/tone.wav"
say "smoke passed"
