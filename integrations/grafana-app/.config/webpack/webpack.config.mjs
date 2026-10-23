import CopyWebpackPlugin from "copy-webpack-plugin";
import path from "path";

const pluginId = "chrismatix-grog-app";
const distDir = path.resolve(process.cwd(), "dist");

export default async function config(env) {
  const isProduction = Boolean(env.production);

  return {
    mode: isProduction ? "production" : "development",
    context: process.cwd(),
    entry: {
      module: "./src/module.tsx",
    },
    devtool: isProduction ? "source-map" : "eval-source-map",
    externals: [/^@grafana\/.+$/, "react", "react/jsx-runtime", "react-dom", "@emotion/css"],
    module: {
      rules: [
        {
          test: /\.[tj]sx?$/,
          exclude: /node_modules/,
          use: {
            loader: "swc-loader",
            options: {
              jsc: {
                parser: {
                  syntax: "typescript",
                  tsx: true,
                },
                transform: {
                  react: {
                    runtime: "automatic",
                  },
                },
                target: "es2018",
              },
            },
          },
        },
        {
          test: /\.css$/,
          use: ["style-loader", "css-loader"],
        },
        {
          test: /\.s[ac]ss$/,
          use: ["style-loader", "css-loader", "sass-loader"],
        },
        {
          test: /\.(png|jpe?g|gif|svg)$/,
          type: "asset/resource",
          generator: {
            filename: "[name][ext]",
          },
        },
      ],
    },
    output: {
      clean: true,
      filename: "[name].js",
      library: {
        type: "amd",
      },
      path: distDir,
      publicPath: `public/plugins/${pluginId}/`,
      uniqueName: pluginId,
    },
    plugins: [
      new CopyWebpackPlugin({
        patterns: [
          { from: "plugin.json", to: "plugin.json" },
          { from: "README.md", to: "README.md", noErrorOnMissing: true },
          { from: "src/img", to: "img", noErrorOnMissing: true },
        ],
      }),
    ],
    resolve: {
      extensions: [".ts", ".tsx", ".js"],
      modules: [path.resolve(process.cwd(), "src"), "node_modules"],
    },
  };
}
