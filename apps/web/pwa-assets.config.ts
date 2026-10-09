import { defineConfig } from "@vite-pwa/assets-generator/config";

const BRAND = "#f38d70";

/**
 * Rasterizes public/icon.svg into the PNG icons referenced by the manifest and index.html.
 * Run with `pnpm pwa:assets` and commit the output. The maskable and Apple icons are filled with
 * the brand color (the source's rounded square then disappears into it), leaving ~20% safe zone.
 */
export default defineConfig({
  headLinkOptions: { preset: "2023" },
  preset: {
    transparent: { sizes: [192, 512], padding: 0.05 },
    maskable: { sizes: [512], padding: 0.2, resizeOptions: { background: BRAND } },
    apple: { sizes: [180], padding: 0, resizeOptions: { background: BRAND } },
    assetName: (type, size) => {
      if (type === "maskable") return `maskable-${size.width}x${size.height}.png`;
      if (type === "apple") return `apple-touch-icon-${size.width}x${size.height}.png`;
      return `pwa-${size.width}x${size.height}.png`;
    },
  },
  images: ["public/icon.svg"],
});
