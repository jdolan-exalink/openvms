import type { Map as MapLibreMap } from "maplibre-gl";

export interface SdfIconDefinition {
  id: string;
  width: number;
  height: number;
  draw: (ctx: CanvasRenderingContext2D) => void;
}

// 24x24 monochrome glyphs that MapLibre will color with icon-color
export const ICONS: SdfIconDefinition[] = [
  {
    id: "cam-normal",
    width: 24,
    height: 24,
    draw: (ctx) => {
      ctx.fillStyle = "#ffffff";
      // Camera body
      ctx.beginPath();
      ctx.roundRect(4, 7, 11, 10, 2);
      ctx.fill();
      // Camera lens cone
      ctx.beginPath();
      ctx.moveTo(15, 9);
      ctx.lineTo(20, 6);
      ctx.lineTo(20, 18);
      ctx.lineTo(15, 15);
      ctx.closePath();
      ctx.fill();
      // Stand / base
      ctx.fillRect(8, 17, 3, 3);
    },
  },
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
