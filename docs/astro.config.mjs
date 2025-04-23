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
          items: ["get-started", "why-grog", "build-configuration"],
        },
        {
          label: "Guides",
          collapsed: false,
          autogenerate: { directory: "guides" },
        },
        {
          label: "Reference",
          collapsed: false,
          autogenerate: { directory: "reference", collapsed: true },
        },
      ],
    }),
  ],
});
