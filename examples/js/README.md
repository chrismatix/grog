# JavaScript Monorepo Example with Grog

This example demonstrates how to use Grog to manage a JavaScript monorepo with shared packages and a Next.js application.

## How to Use

### Building All Packages

To build all packages in the monorepo:

```bash
grog build //...
```

Or use the specific target:

```bash
grog build :build_all
```

### Building Individual Packages

To build a specific package:

```bash
# Build the UI components
grog build //packages/ui-components:build

# Build the utilities
grog build //packages/utils:build

# Build the theme
grog build //packages/theme:build

# Build the Next.js application
grog build //next.js:build
```

### Development Mode

To run all shared packages in development mode:

```bash
grog build :dev
```

## Workspaces Configuration

This monorepo uses npm/yarn workspaces to enable local package resolution. The root `package.json` contains:

```json
{
  "workspaces": [
    "packages/*",
    "next.js"
  ]
}
```

This configuration allows packages to reference each other using the `@monorepo` namespace without having to publish them to a registry. For example, in the Next.js application:

```tsx
import { Button } from '@monorepo/ui-components';
import { isValidEmail } from '@monorepo/utils';
import { colors } from '@monorepo/theme';
```

When you run `npm install` or `yarn install` at the root of the monorepo, the package manager creates symlinks in the `node_modules` directory that point to the local packages, enabling seamless imports.
