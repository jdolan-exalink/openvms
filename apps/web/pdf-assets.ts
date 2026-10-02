import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import type { Plugin } from "vite";

/** Keep PDF.js runtime resources version-matched and same-origin. */
export function localPdfAssets(): Plugin {
  const prefix = "assets/pdfjs-6.3.289/";
  const assets = new Map<string, Buffer>();
  for (const folder of ["cmaps", "standard_fonts", "wasm", "iccs"]) {
    const directory = new URL(`./node_modules/pdfjs-dist/${folder}/`, import.meta.url);
    for (const name of readdirSync(directory)) {
      if (name === "LICENSE" || name.endsWith(".js") || name.endsWith(".map")) continue;
      assets.set(`${prefix}${folder}/${name}`, readFileSync(fileURLToPath(new URL(name,directory))));
    }
  }
  return {
    name: "local-pdf-assets",
    generateBundle() { for (const [fileName, source] of assets) this.emitFile({type:"asset",fileName,source}); },
    configureServer(server) {
      server.middlewares.use((request,response,next) => {
        const asset = assets.get((request.url ?? "").split("?")[0]?.replace(/^\//,"") ?? "");
        if (!asset) return next();
        response.setHeader("Content-Type", "application/octet-stream");
        response.end(asset);
      });
    },
  };
}
