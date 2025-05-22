---
title: GitHub Actions Integration
description: Learn how to integrate Grog builds and tests into your GitHub Actions CI/CD workflows.
---

# Using Grog with GitHub Actions

GitHub Actions provides powerful CI/CD capabilities that work seamlessly with Grog. This guide shows you how to set up GitHub Actions workflows that leverage Grog's build system for faster, more efficient CI pipelines.

## Basic Setup

Here's a simple GitHub Actions workflow that installs Grog and runs your builds:

```yaml
name: Build with Grog

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v3

      - name: Install Grog
        run: |
          curl -L https://grog.build/latest/grog-linux-amd64 -o /usr/local/bin/grog
          chmod +x /usr/local/bin/grog
          grog version

      - name: Build with Grog
        run: grog build //...
```

Save this as `.github/workflows/build.yml` in your repository.

## Optimizing CI Builds with Incremental Builds

One of Grog's strengths is its ability to build only what has changed.
Here's how to leverage this in GitHub Actions:

```yaml
name: Incremental Build

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v3
        with:
          fetch-depth: 0 # Needed to access commit history

      - name: Install Grog
        run: |
          curl -L https://grog.build/latest/grog-linux-amd64 -o /usr/local/bin/grog
          chmod +x /usr/local/bin/grog

      - name: Build changed targets
        run: |
          # For pull requests, compare against the base branch
          if [ "${{ github.event_name }}" == "pull_request" ]; then
            BASE_COMMIT=${{ github.event.pull_request.base.sha }}
          else
            # For pushes, compare against the previous commit
            BASE_COMMIT=$(git rev-parse HEAD~1)
          fi

          # Build only what changed
          CHANGED_TARGETS=$(grog changes --since=$BASE_COMMIT --dependents=transitive)

          if [ -n "$CHANGED_TARGETS" ]; then
            echo "Building changed targets: $CHANGED_TARGETS"
            grog build $CHANGED_TARGETS
          else
            echo "No targets changed, skipping build"
          fi
```

## Caching Build Outputs

To speed up your CI builds, you can use GitHub's cache action along with Grog's local cache:

```yaml
name: Build with Cache

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v3

      - name: Install Grog
        run: |
          curl -L https://grog.build/latest/grog-linux-amd64 -o /usr/local/bin/grog
          chmod +x /usr/local/bin/grog

      - name: Cache Grog build outputs
        uses: actions/cache@v3
        with:
          path: ~/.grog
          key: ${{ runner.os }}-grog-${{ hashFiles('**/BUILD.*') }}
          restore-keys: |
            ${{ runner.os }}-grog-

      - name: Build with Grog
        run: grog build //...
```

## Remote Caching with GCS or S3

For more robust caching, especially across different workflows and branches, you can use Grog's remote caching capabilities:

```yaml
name: Build with Remote Cache

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v3

      - name: Install Grog
        run: |
          curl -L https://grog.build/latest/grog-linux-amd64 -o /usr/local/bin/grog
          chmod +x /usr/local/bin/grog

      - name: Set up GCP authentication
        uses: google-github-actions/auth@v1
        with:
          credentials_json: ${{ secrets.GCP_SA_KEY }}

      - name: Configure Grog remote cache
        run: |
          cat > grog.toml << EOF
          [cache]
          backend = "gcs"

          [cache.gcs]
          bucket = "your-gcs-bucket-name"
          prefix = "grog-cache/"
          EOF

      - name: Build with Grog
        run: grog build //...
```

Make sure to store your GCP service account key as a GitHub secret.

## Running Tests

To run tests with Grog in GitHub Actions:

```yaml
name: Test

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v3

      - name: Install Grog
        run: |
          curl -L https://grog.build/latest/grog-linux-amd64 -o /usr/local/bin/grog
          chmod +x /usr/local/bin/grog

      - name: Run tests
        run: grog test //...

      # Optionally, you can run only changed tests
      - name: Run changed tests
        run: |
          if [ "${{ github.event_name }}" == "pull_request" ]; then
            BASE_COMMIT=${{ github.event.pull_request.base.sha }}
          else
            BASE_COMMIT=$(git rev-parse HEAD~1)
          fi

          CHANGED_TESTS=$(grog changes --since=$BASE_COMMIT --dependents=transitive | grep "test$")

          if [ -n "$CHANGED_TESTS" ]; then
            echo "Running changed tests: $CHANGED_TESTS"
            grog test $CHANGED_TESTS
          else
            echo "No tests changed, skipping test run"
          fi
```

## Multi-platform Builds

If you need to build for multiple platforms, you can use GitHub Actions' matrix strategy:

```yaml
name: Multi-platform Build

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - name: Checkout code
        uses: actions/checkout@v3

      - name: Install Grog (Linux)
        if: matrix.os == 'ubuntu-latest'
        run: |
          curl -L https://grog.build/latest/grog-linux-amd64 -o /usr/local/bin/grog
          chmod +x /usr/local/bin/grog

      - name: Install Grog (macOS)
        if: matrix.os == 'macos-latest'
        run: |
          curl -L https://grog.build/latest/grog-darwin-amd64 -o /usr/local/bin/grog
          chmod +x /usr/local/bin/grog

      - name: Install Grog (Windows)
        if: matrix.os == 'windows-latest'
        run: |
          curl -L https://grog.build/latest/grog-windows-amd64.exe -o grog.exe
          echo "$PWD" >> $GITHUB_PATH

      - name: Build with Grog
        run: grog build //...
```

## Docker Image Builds

If your project uses Docker, you can integrate Grog's Docker caching with GitHub Actions:

```yaml
name: Docker Build

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v3

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v2

      - name: Install Grog
        run: |
          curl -L https://grog.build/latest/grog-linux-amd64 -o /usr/local/bin/grog
          chmod +x /usr/local/bin/grog

      - name: Configure Grog for Docker
        run: |
          cat > grog.toml << EOF
          [docker]
          enabled = true
          EOF

      - name: Build Docker images with Grog
        run: grog build //...
```

## Best Practices

1. **Use incremental builds**: Only build what has changed to speed up CI
2. **Enable caching**: Use GitHub's cache action or Grog's remote caching
3. **Parallelize when possible**: Use GitHub Actions' matrix strategy for independent builds
4. **Keep workflows focused**: Create separate workflows for different purposes (build, test, deploy)
5. **Use environment variables**: Store configuration in GitHub secrets and environment variables
6. **Add status checks**: Configure required status checks to protect your main branch

## Troubleshooting

### Common Issues

#### "grog: command not found"

Make sure the Grog binary is installed correctly and is in the PATH.

#### Slow builds

- Enable caching
- Use incremental builds with `grog changes`
- Consider using remote caching

#### Cache misses

- Ensure cache keys are consistent
- Check if your workflow is using the correct cache configuration
- Verify that your build inputs are deterministic

## Complete Example

Here's a complete example that combines many of the techniques above:

```yaml
name: Grog CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build-and-test:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v3
        with:
          fetch-depth: 0

      - name: Install Grog
        run: |
          curl -L https://grog.build/latest/grog-linux-amd64 -o /usr/local/bin/grog
          chmod +x /usr/local/bin/grog

      - name: Cache Grog build outputs
        uses: actions/cache@v3
        with:
          path: ~/.grog
          key: ${{ runner.os }}-grog-${{ hashFiles('**/BUILD.*') }}
          restore-keys: |
            ${{ runner.os }}-grog-

      - name: Determine changed targets
        id: changes
        run: |
          if [ "${{ github.event_name }}" == "pull_request" ]; then
            BASE_COMMIT=${{ github.event.pull_request.base.sha }}
          else
            BASE_COMMIT=$(git rev-parse HEAD~1)
          fi

          CHANGED_TARGETS=$(grog changes --since=$BASE_COMMIT --dependents=transitive)
          echo "targets=$CHANGED_TARGETS" >> $GITHUB_OUTPUT

      - name: Build changed targets
        if: steps.changes.outputs.targets != ''
        run: grog build ${{ steps.changes.outputs.targets }}

      - name: Run changed tests
        if: steps.changes.outputs.targets != ''
        run: |
          CHANGED_TESTS=$(echo "${{ steps.changes.outputs.targets }}" | grep "test$" || true)
          if [ -n "$CHANGED_TESTS" ]; then
            grog test $CHANGED_TESTS
          else
            echo "No tests changed, skipping test run"
          fi
```

This workflow:

1. Checks out your code with full history
2. Installs Grog
3. Sets up caching
4. Determines which targets have changed
5. Builds only the changed targets
6. Runs only the changed tests

By following these patterns, you can create efficient, reliable CI/CD pipelines with GitHub Actions and Grog.
