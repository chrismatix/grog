// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import starlightThemeRapide from "starlight-theme-rapide";
import * as fs from "node:fs";
import mermaid from "astro-mermaid";

// https://astro.build/config
export default defineConfig({
  site: "https://grog.build",
  integrations: [
    mermaid({
      theme: "base",
    }),
    starlight({
      title: "Grog Docs",
      plugins: [starlightThemeRapide()],
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/chrismatix/grog",
        },
        {
          icon: "slack",
          label: "Slack",
          href: "https://join.slack.com/t/grog-build/shared_invite/zt-3vipu1c5w-9ouz0nDV0YNKYIqskMgv5Q",
        },
      ],
      sidebar: [
        {
          label: "Start Here!",
          items: ["get-started", "why-grog", "build-configuration"],
        },
        {
          label: "Topics",
          collapsed: false,
          autogenerate: { directory: "topics" },
        },
        {
          label: "Tracing",
          collapsed: false,
          items: [
            "tracing",
            {
              label: "Integrations",
              collapsed: true,
              autogenerate: { directory: "tracing/integrations" },
            },
          ],
        },
        {
          label: "Reference",
          collapsed: false,
          autogenerate: { directory: "reference", collapsed: true },
        },
      ],
      expressiveCode: {
        shiki: {
          langs: [JSON.parse(fs.readFileSync("./pkl_grammar.json", "utf-8"))],
        },
      },
    }),
  ],
});
