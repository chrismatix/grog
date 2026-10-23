import { AppPlugin } from "@grafana/data";

import { App } from "./App";
import { ConfigPage } from "./ConfigPage";
import type { AppSettings } from "./sql/client";

export const plugin = new AppPlugin<AppSettings>().setRootPage(App).addConfigPage({
  title: "Configuration",
  icon: "cog",
  body: ConfigPage,
  id: "configuration",
});
