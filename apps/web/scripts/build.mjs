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
mkdirSync("dist", { recursive: true });
copyFileSync("index.html", "dist/index.html");
