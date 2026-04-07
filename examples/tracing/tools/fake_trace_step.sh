#!/bin/sh

set -eu

step_name="$1"
output_path="$2"
shift 2

mkdir -p "$(dirname "$output_path")"

signature_input="$step_name"
line_count=0

for input_path in "$@"; do
  if [ -f "$input_path" ]; then
    file_signature="$(cksum "$input_path" | awk '{print $1 ":" $2}')"
    file_lines="$(wc -l < "$input_path" | tr -d ' ')"
    line_count=$((line_count + file_lines))
    signature_input="$signature_input|$input_path|$file_signature"
  else
    signature_input="$signature_input|$input_path|missing"
  fi
done

signature="$(printf '%s' "$signature_input" | cksum | awk '{print $1}')"
queue_wait_ms=$((signature % 260 + 40))
work_time_ms=$(((signature / 7) % 900 + 180))
spike_score=$(((signature / 13) % 70 + 25))
cache_pressure=$(((signature / 17) % 45 + 40))
alert_count=$(((signature / 23) % 4 + 1))

sleep "$(awk -v milliseconds="$queue_wait_ms" 'BEGIN { printf "%.3f", milliseconds / 1000 }')"
sleep "$(awk -v milliseconds="$work_time_ms" 'BEGIN { printf "%.3f", milliseconds / 1000 }')"

top_signal="checkout-api"
case "$step_name" in
  ingest_checkout_events)
    top_signal="payment retries"
    ;;
  sample_api_latencies)
    top_signal="fraud-api tail latency"
    ;;
  score_cache_health)
    top_signal="recommendations cache churn"
    ;;
  correlate_hotspots)
    top_signal="fraud-api and recommendations cache"
    ;;
  write_weekly_brief)
    top_signal="promo traffic plus fraud rollout"
    ;;
  publish_demo_bundle)
    top_signal="clustered checkout slowdown"
    ;;
  publish_demo_bundle_test)
    top_signal="bundle integrity and rollout confidence"
    ;;
esac

if [ "${output_path##*.}" = "md" ]; then
  cat > "$output_path" <<EOF
# ${step_name}

- pseudo_queue_wait_ms: ${queue_wait_ms}
- pseudo_work_time_ms: ${work_time_ms}
- spike_score: ${spike_score}
- cache_pressure: ${cache_pressure}
- alert_count: ${alert_count}
- top_signal: ${top_signal}
- contributing_inputs: ${line_count}

This file is intentionally synthetic. It gives the tracing example a believable
storyline so exported traces show varied durations and a few downstream steps
that read earlier artifacts before producing a report.
EOF
else
  cat > "$output_path" <<EOF
{
  "step": "${step_name}",
  "pseudo_queue_wait_ms": ${queue_wait_ms},
  "pseudo_work_time_ms": ${work_time_ms},
  "spike_score": ${spike_score},
  "cache_pressure": ${cache_pressure},
  "alert_count": ${alert_count},
  "top_signal": "${top_signal}",
  "contributing_inputs": ${line_count},
  "signature": ${signature}
}
EOF
fi
