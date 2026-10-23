#!/usr/bin/env bash
# Prints the running GitHub Actions job and its finished steps as OTLP/JSON spans.
set -euo pipefail

trace_id=$(od -An -tx1 -N16 /dev/urandom | tr -d ' \n')
root_span_id=$(od -An -tx1 -N8 /dev/urandom | tr -d ' \n')

curl --fail --silent --show-error \
	--header "Authorization: Bearer ${GITHUB_TOKEN}" \
	--header "Accept: application/vnd.github+json" \
	"${GITHUB_API_URL}/repos/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}/attempts/${GITHUB_RUN_ATTEMPT}/jobs?per_page=100" |
	jq --compact-output \
		--arg runner "${RUNNER_NAME}" \
		--arg trace_id "${trace_id}" \
		--arg root_span_id "${root_span_id}" \
		--arg workflow "${GITHUB_WORKFLOW}" \
		--arg job "${GITHUB_JOB}" \
		--arg repository "${GITHUB_REPOSITORY}" \
		--arg run_id "${GITHUB_RUN_ID}" \
		--arg run_attempt "${GITHUB_RUN_ATTEMPT}" \
		--arg ref_name "${GITHUB_REF_NAME}" \
		--arg head_ref "${GITHUB_HEAD_REF:-}" \
		--arg event_name "${GITHUB_EVENT_NAME}" '
    def nanos: fromdateiso8601 | tostring + "000000000";
    def attribute($key; $value): {key: $key, value: {stringValue: ($value | tostring)}};
    (now | floor | tostring + "000000000") as $now
    | [.jobs[] | select(.runner_name == $runner and .status == "in_progress")][0]
    | select(. != null)
    | . as $current
    | {
        resourceSpans: [{
          resource: {attributes: [attribute("service.name"; "github-actions")]},
          scopeSpans: [{
            scope: {name: "github-job-spans"},
            spans: ([{
              traceId: $trace_id,
              spanId: $root_span_id,
              name: $current.name,
              kind: 1,
              startTimeUnixNano: ($current.started_at | nanos),
              endTimeUnixNano: $now,
              status: {code: (if any($current.steps[]; .conclusion == "failure") then 2 else 1 end)},
              attributes: [
                attribute("github.workflow"; $workflow),
                attribute("github.job"; $job),
                attribute("github.job_name"; $current.name),
                attribute("github.job_id"; $current.id),
                attribute("github.repository"; $repository),
                attribute("github.run_id"; $run_id),
                attribute("github.run_attempt"; $run_attempt),
                attribute("github.ref_name"; $ref_name),
                attribute("github.head_ref"; $head_ref),
                attribute("github.event_name"; $event_name),
                attribute("github.runner_name"; $current.runner_name),
                attribute("github.runner_labels"; $current.labels | join(",")),
                attribute("github.queue_ms";
                  (($current.started_at | fromdateiso8601) - ($current.created_at | fromdateiso8601)) * 1000)
              ]
            }] + [
              $current.steps[]
              | select(.started_at != null and .completed_at != null and .conclusion != "skipped")
              | {
                  traceId: $trace_id,
                  spanId: ("0000000000000000" + (.number | tostring))[-16:],
                  parentSpanId: $root_span_id,
                  name: .name,
                  kind: 1,
                  startTimeUnixNano: (.started_at | nanos),
                  endTimeUnixNano: (.completed_at | nanos),
                  status: {code: (if .conclusion == "failure" then 2 else 1 end)},
                  attributes: [
                    attribute("github.step_number"; .number),
                    attribute("github.conclusion"; .conclusion)
                  ]
                }
            ])
          }]
        }]
      }'
