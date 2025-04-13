// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import starlightThemeRapide from "starlight-theme-rapide";

// https://astro.build/config
export default defineConfig({
  integrations: [
    starlight({
      title: "Grog Docs",
      plugins: [starlightThemeRapide()],
      social: {
        github: "https://github.com/chrismatix/grog",
      },
      sidebar: [
        {
          label: "Start Here!",
          items: ["getting-started", "why-grog"],
        },
        {
          label: "Reference",
          collapsed: false,
          autogenerate: { directory: "reference", collapsed: true },
        },
        {
          label: "Guides",
          collapsed: true,
          autogenerate: { directory: "guides" },
        },
      ],
    }),
  ],
});
