import type { Map as MapLibreMap } from "maplibre-gl";

export interface SdfIconDefinition {
  id: string;
  width: number;
  height: number;
  draw: (ctx: CanvasRenderingContext2D) => void;
}

/**
 * Lucide glyphs (24px grid, stroke 2, round caps) as static SVG path data. MapLibre only
 * reads the alpha channel of SDF sprites and recolors them with icon-color, so the white
 * used while drawing is a mask value, not a theme color.
 */
const LUCIDE_PATHS = {
  video: [
    "m16 13 5.223 3.482a.5.5 0 0 0 .777-.416V7.87a.5.5 0 0 0-.752-.432L16 10.5",
    "M4 6h10a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2z",
  ],
  building: [
    "M12 10h.01", "M12 14h.01", "M12 6h.01", "M16 10h.01", "M16 14h.01", "M16 6h.01", "M8 10h.01", "M8 14h.01", "M8 6h.01",
    "M9 22v-3a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v3",
    "M6 2h12a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z",
  ],
  server: [
    "M4 2h16a2 2 0 0 1 2 2v4a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z",
    "M4 14h16a2 2 0 0 1 2 2v4a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-4a2 2 0 0 1 2-2z",
    "M6 6h.01", "M6 18h.01",
  ],
  x: ["M18 6 6 18", "m6 6 12 12"],
} as const;

function lucideGlyph(id: string, paths: readonly string[]): SdfIconDefinition {
  return { id, width: 24, height: 24, draw: ctx => {
    // Glyph occupies 20px of the 24px sprite so the SDF keeps a margin around the stroke.
    const scale = 20 / 24;
    ctx.save();
    ctx.translate(2, 2);
    ctx.scale(scale, scale);
    ctx.strokeStyle = "#ffffff";
    ctx.lineWidth = 2.4;
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    for (const path of paths) ctx.stroke(new Path2D(path));
    ctx.restore();
  } };
}

// 24x24 monochrome glyphs that MapLibre will color with icon-color
export const ICONS: SdfIconDefinition[] = [
  { id: "cam-dome", width: 24, height: 24, draw: (ctx) => {
    ctx.fillStyle = "#ffffff"; ctx.beginPath(); ctx.arc(12, 12, 8, Math.PI, 0); ctx.lineTo(20, 17);
    ctx.lineTo(4, 17); ctx.closePath(); ctx.fill(); ctx.fillRect(9, 17, 6, 3);
  } },
  { id: "cam-ptz", width: 24, height: 24, draw: (ctx) => {
    ctx.fillStyle = "#ffffff"; ctx.beginPath(); ctx.arc(12, 10, 7, 0, Math.PI * 2); ctx.fill();
    ctx.fillRect(10, 16, 4, 4); ctx.fillRect(7, 20, 10, 2);
  } },
  lucideGlyph("cam-normal", LUCIDE_PATHS.video),
  lucideGlyph("site-building", LUCIDE_PATHS.building),
  lucideGlyph("server", LUCIDE_PATHS.server),
  lucideGlyph("status-offline", LUCIDE_PATHS.x),
  {
    id: "cam-alarm",
    width: 24,
    height: 24,
    draw: (ctx) => {
      ctx.fillStyle = "#ffffff";
      // Alarm bell shape
      ctx.beginPath();
      ctx.arc(12, 10, 5, Math.PI, 0, false);
      ctx.lineTo(18, 16);
      ctx.lineTo(6, 16);
      ctx.closePath();
      ctx.fill();
      // Bell clapper
      ctx.beginPath();
      ctx.arc(12, 17.5, 2, 0, Math.PI * 2);
      ctx.fill();
      // Top ring
      ctx.beginPath();
      ctx.arc(12, 4.5, 1.5, 0, Math.PI * 2);
      ctx.fill();
    },
  },
  {
    id: "cam-warning",
    width: 24,
    height: 24,
    draw: (ctx) => {
      ctx.fillStyle = "#ffffff";
      // Warning triangle
      ctx.beginPath();
      ctx.moveTo(12, 4);
      ctx.lineTo(21, 19);
      ctx.lineTo(3, 19);
      ctx.closePath();
      ctx.fill();
      // Exclamation cutout (drawn as black in mask)
      ctx.fillStyle = "#000000";
      ctx.fillRect(11, 8, 2, 6);
      ctx.fillRect(11, 16, 2, 2);
    },
  },
  {
    id: "cam-offline",
    width: 24,
    height: 24,
    draw: (ctx) => {
      ctx.fillStyle = "#ffffff";
      // Camera body
      ctx.beginPath();
      ctx.roundRect(4, 7, 11, 10, 2);
      ctx.fill();
      ctx.beginPath();
      ctx.moveTo(15, 9);
      ctx.lineTo(20, 6);
      ctx.lineTo(20, 18);
      ctx.lineTo(15, 15);
      ctx.closePath();
      ctx.fill();
      // Slash line across camera
      ctx.strokeStyle = "#ffffff";
      ctx.lineWidth = 2.5;
      ctx.beginPath();
      ctx.moveTo(3, 21);
      ctx.lineTo(21, 3);
      ctx.stroke();
    },
  },
  {
    id: "cam-server-offline",
    width: 24,
    height: 24,
    draw: (ctx) => {
      ctx.fillStyle = "#ffffff";
      // Server racks stacked
      ctx.roundRect(5, 5, 14, 4, 1);
      ctx.roundRect(5, 10, 14, 4, 1);
      ctx.roundRect(5, 15, 14, 4, 1);
      ctx.fill();
    },
  },
];

export function registerSdfSprites(map: MapLibreMap) {
  if (typeof document === "undefined") return;

  const canvas = document.createElement("canvas");
  const ctx = canvas.getContext("2d", { willReadFrequently: true });
  if (!ctx) return;

  for (const icon of ICONS) {
    if (map.hasImage(icon.id)) continue;

    canvas.width = icon.width;
    canvas.height = icon.height;
    ctx.clearRect(0, 0, icon.width, icon.height);

    icon.draw(ctx);

    const imgData = ctx.getImageData(0, 0, icon.width, icon.height);
    try {
      map.addImage(icon.id, imgData, { sdf: true });
    } catch {
      // Ignored if already added
    }
  }
}
