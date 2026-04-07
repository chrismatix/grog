target(
    name = "ingest_checkout_events",
    command = "sh tools/fake_trace_step.sh ingest_checkout_events out/checkout_ingest.json signals/checkout_events.csv",
    inputs = [
        "signals/checkout_events.csv",
        "tools/fake_trace_step.sh",
    ],
    outputs = ["out/checkout_ingest.json"],
)

target(
    name = "sample_api_latencies",
    command = "sh tools/fake_trace_step.sh sample_api_latencies out/api_latency_profile.json signals/api_latency.csv",
    inputs = [
        "signals/api_latency.csv",
        "tools/fake_trace_step.sh",
    ],
    outputs = ["out/api_latency_profile.json"],
)

target(
    name = "score_cache_health",
    command = "sh tools/fake_trace_step.sh score_cache_health out/cache_health_score.json signals/cache_health.txt",
    inputs = [
        "signals/cache_health.txt",
        "tools/fake_trace_step.sh",
    ],
    outputs = ["out/cache_health_score.json"],
)

target(
    name = "correlate_hotspots",
    command = "sh tools/fake_trace_step.sh correlate_hotspots reports/hotspots.md out/checkout_ingest.json out/api_latency_profile.json out/cache_health_score.json",
    dependencies = [
        ":ingest_checkout_events",
        ":sample_api_latencies",
        ":score_cache_health",
    ],
    inputs = ["tools/fake_trace_step.sh"],
    outputs = ["reports/hotspots.md"],
)

target(
    name = "write_weekly_brief",
    command = "sh tools/fake_trace_step.sh write_weekly_brief reports/weekly_brief.md reports/hotspots.md notes/release_context.txt",
    dependencies = [":correlate_hotspots"],
    inputs = [
        "notes/release_context.txt",
        "tools/fake_trace_step.sh",
    ],
    outputs = ["reports/weekly_brief.md"],
)

target(
    name = "publish_demo_bundle",
    command = "sh tools/fake_trace_step.sh publish_demo_bundle dist/demo_bundle.txt reports/hotspots.md reports/weekly_brief.md out/checkout_ingest.json out/api_latency_profile.json out/cache_health_score.json",
    dependencies = [
        ":correlate_hotspots",
        ":write_weekly_brief",
    ],
    inputs = ["tools/fake_trace_step.sh"],
    outputs = ["dist/demo_bundle.txt"],
)

target(
    name = "publish_demo_bundle_test",
    command = "sh tools/fake_trace_step.sh publish_demo_bundle_test dist/demo_test_report.txt dist/demo_bundle.txt reports/weekly_brief.md",
    dependencies = [":publish_demo_bundle"],
    inputs = ["tools/fake_trace_step.sh"],
    outputs = ["dist/demo_test_report.txt"],
)
