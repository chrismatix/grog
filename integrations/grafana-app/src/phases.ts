export const phases = [
  { key: "queue_wait_ms", label: "Queue", color: "purple" },
  { key: "hash_duration_ms", label: "Hash", color: "yellow" },
  { key: "cache_check_ms", label: "Cache check", color: "orange" },
  { key: "dep_load_ms", label: "Dep load", color: "light-blue" },
  { key: "command_duration_ms", label: "Command", color: "blue" },
  { key: "output_write_ms", label: "Output write", color: "green" },
  { key: "output_load_ms", label: "Output load", color: "super-light-green" },
  { key: "cache_write_ms", label: "Cache write", color: "dark-green" },
] as const;

export const cachePhaseKeys = [
  "cache_check_ms",
  "dep_load_ms",
  "output_load_ms",
  "cache_write_ms",
] as const;
