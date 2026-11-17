import * as esbuild from "esbuild";
import { copyFileSync, mkdirSync } from "node:fs";

await esbuild.build({
  entryPoints: ["src/main.ts"],
  bundle: true,
  outfile: "dist/viewer.js",
  format: "iife",
  target: "es2022",
  legalComments: "none",
});
await esbuild.build({
  entryPoints: ["src/operator-page.ts"],
  bundle: true,
  outfile: "dist/operator.js",
  format: "iife",
  target: "es2022",
  legalComments: "none",
});
mkdirSync("dist", { recursive: true });
copyFileSync("index.html", "dist/index.html");
copyFileSync("operator.html", "dist/operator.html");
