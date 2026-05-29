package tracing

// BuildRow represents one row in the builds Parquet table.
type BuildRow struct {
	TraceID                 string `parquet:"trace_id" json:"trace_id"`
	Workspace               string `parquet:"workspace" json:"workspace"`
	GitCommit               string `parquet:"git_commit" json:"git_commit"`
	GitBranch               string `parquet:"git_branch" json:"git_branch"`
	GrogVersion             string `parquet:"grog_version" json:"grog_version"`
	Platform                string `parquet:"platform" json:"platform"`
	Command                 string `parquet:"command" json:"command"`
	StartTimeUnixMillis     int64  `parquet:"start_time_unix_millis" json:"start_time_unix_millis"`
	TotalDurationMillis     int64  `parquet:"total_duration_millis" json:"total_duration_millis"`
	TotalTargets            int32  `parquet:"total_targets" json:"total_targets"`
	SuccessCount            int32  `parquet:"success_count" json:"success_count"`
	FailureCount            int32  `parquet:"failure_count" json:"failure_count"`
	CacheHitCount           int32  `parquet:"cache_hit_count" json:"cache_hit_count"`
	CriticalPathExecMillis  int64  `parquet:"critical_path_exec_millis" json:"critical_path_exec_millis"`
	CriticalPathCacheMillis int64  `parquet:"critical_path_cache_millis" json:"critical_path_cache_millis"`
	AsyncCacheWaitMillis    int64  `parquet:"async_cache_wait_millis" json:"async_cache_wait_millis"`
	IsCI                    bool   `parquet:"is_ci" json:"is_ci"`
	RequestedPatterns       string `parquet:"requested_patterns" json:"requested_patterns"`
}

// SpanRow represents one row in the spans Parquet table.
type SpanRow struct {
	TraceID               string `parquet:"trace_id" json:"trace_id"`
	Label                 string `parquet:"label" json:"label"`
	Package               string `parquet:"package" json:"package"`
	ChangeHash            string `parquet:"change_hash" json:"change_hash"`
	OutputHash            string `parquet:"output_hash" json:"output_hash"`
	Status                string `parquet:"status" json:"status"`             // SUCCESS, FAILURE, CANCELLED
	CacheResult           string `parquet:"cache_result" json:"cache_result"` // CACHE_HIT, CACHE_MISS, CACHE_SKIP
	Command               string `parquet:"command" json:"command"`
	ExitCode              int32  `parquet:"exit_code" json:"exit_code"`
	IsTest                bool   `parquet:"is_test" json:"is_test"`
	StartTimeUnixMillis   int64  `parquet:"start_time_unix_millis" json:"start_time_unix_millis"`
	EndTimeUnixMillis     int64  `parquet:"end_time_unix_millis" json:"end_time_unix_millis"`
	TotalDurationMillis   int64  `parquet:"total_duration_millis" json:"total_duration_millis"`
	QueueWaitMillis       int64  `parquet:"queue_wait_millis" json:"queue_wait_millis"`
	HashDurationMillis    int64  `parquet:"hash_duration_millis" json:"hash_duration_millis"`
	CacheCheckMillis      int64  `parquet:"cache_check_millis" json:"cache_check_millis"`
	CommandDurationMillis int64  `parquet:"command_duration_millis" json:"command_duration_millis"`
	OutputWriteMillis     int64  `parquet:"output_write_millis" json:"output_write_millis"`
	OutputLoadMillis      int64  `parquet:"output_load_millis" json:"output_load_millis"`
	CacheWriteMillis      int64  `parquet:"cache_write_millis" json:"cache_write_millis"`
	DepLoadMillis         int64  `parquet:"dep_load_millis" json:"dep_load_millis"`
	Tags                  string `parquet:"tags" json:"tags"`
	Dependencies          string `parquet:"dependencies" json:"dependencies"`
}

// BuildTrace is the in-memory representation of a complete trace.
type BuildTrace struct {
	Build BuildRow
	Spans []SpanRow
}
