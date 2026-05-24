provider "grog" {
  # Path to the directory containing grog.toml. If omitted, the provider walks
  # up from the current working directory to find grog.toml. Set explicitly
  # under Terragrunt, where the working directory is usually not the repo root.
  workspace_root = "${path.module}/.."
}
