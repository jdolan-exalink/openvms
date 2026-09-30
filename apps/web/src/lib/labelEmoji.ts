import { knownLabels, labelName } from "@/lib/format";

/** Emoji per Frigate object label; unknown labels fall back to a generic marker. */
export const labelEmojis: Record<string, string> = {
  person: "🧍", car: "🚗", truck: "🚚", bus: "🚌", motorcycle: "🏍️", bicycle: "🚲", dog: "🐕", cat: "🐈", bird: "🐦",
  horse: "🐎", cow: "🐄", sheep: "🐑", bear: "🐻", deer: "🦌", fox: "🦊", raccoon: "🦝", rabbit: "🐇", skunk: "🦨",
  squirrel: "🐿️", face: "🙂", license_plate: "🪪", package: "📦", boat: "⛵", airplane: "✈️", train: "🚆",
  umbrella: "☂️", backpack: "🎒", handbag: "👜", suitcase: "🧳", bottle: "🍾", knife: "🔪", cell_phone: "📱",
  laptop: "💻", skateboard: "🛹", kite: "🪁", frisbee: "🥏", amazon: "📦", ups: "📦", fedex: "📦", usps: "📦",
  dhl: "📦", an_post: "📦", purolator: "📦", postnl: "📦", nzpost: "📦", postnord: "📦", gls: "📦", dpd: "📦",
  royal_mail: "📦", canada_post: "📦", waste_bin: "🗑️", robot_lawnmower: "🤖", gun: "🔫",
};

export const FALLBACK_EMOJI = "🔹";

/** Spanish names for labels that lib/format.ts does not name yet. */
const extraNames: Record<string, string> = {
  bird: "Ave", horse: "Caballo", cow: "Vaca", sheep: "Oveja", bear: "Oso", deer: "Ciervo", fox: "Zorro", raccoon: "Mapache",
  rabbit: "Conejo", skunk: "Zorrillo", squirrel: "Ardilla", boat: "Barco", airplane: "Avión", train: "Tren",
  umbrella: "Paraguas", backpack: "Mochila", handbag: "Bolso", suitcase: "Valija", bottle: "Botella", knife: "Cuchillo",
  cell_phone: "Celular", laptop: "Notebook", skateboard: "Skate", kite: "Barrilete", frisbee: "Frisbee",
  waste_bin: "Cesto de basura", robot_lawnmower: "Cortacésped robot", gun: "Arma",
};

export const labelEmoji = (l: string): string => labelEmojis[l] ?? FALLBACK_EMOJI;

/** Spanish display name: lib/format.ts first, then the extras above, else the raw label. */
export function labelDisplay(l: string): string {
  const base = labelName(l);
  return base !== l ? base : (extraNames[l] ?? l);
}

/** pickerLabels is the union offered by the picker: known labels, emoji-mapped ones and any extras (config, selection). */
export function pickerLabels(...extra: (string[] | undefined)[]): string[] {
  return [...new Set([...knownLabels, ...Object.keys(labelEmojis).filter((l) => l in extraNames || knownLabels.includes(l)), ...extra.flatMap((e) => e ?? [])])];
}
