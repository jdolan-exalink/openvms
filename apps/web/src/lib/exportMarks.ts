/** Where a clip to export starts and ends, and which mark is being placed. */
export type ExportMarks = {
  /** Locked start, in unix seconds. */
  start?: number;
  /** Locked end, in unix seconds. */
  end?: number;
  /** The mark that follows the playhead until it is fixed with a second press. */
  placing?: "start" | "end";
};

export type MarkPress = { marks: ExportMarks; refused?: "end_before_start" | "start_after_end" };

/** What the user sees for each mark: the playhead while it is being placed, else the locked value. */
export function shownMarks(m: ExportMarks, position: number): { start?: number; end?: number } {
  return { start: m.placing === "start" ? position : m.start, end: m.placing === "end" ? position : m.end };
}

/** Inicio can be pressed unless Fin is being placed. */
export const canPressStart = (m: ExportMarks) => m.placing !== "end";

/** Fin can be pressed once Inicio is fixed, and not while Inicio is being placed. */
export const canPressEnd = (m: ExportMarks) => m.start !== undefined && m.placing !== "start";

/** The range can be exported once both marks are fixed in order and nothing is being placed. */
export function exportRange(m: ExportMarks): { start: number; end: number } | undefined {
  if (m.placing || m.start === undefined || m.end === undefined || m.end <= m.start) return undefined;
  return { start: m.start, end: m.end };
}

/** First press: the start mark begins following the playhead. Second press: it is fixed where the playhead is. */
export function pressStart(m: ExportMarks, position: number): MarkPress {
  if (!canPressStart(m)) return { marks: m };
  if (m.placing !== "start") return { marks: { ...m, placing: "start" } };
  if (m.end !== undefined && position >= m.end) return { marks: m, refused: "start_after_end" };
  return { marks: { start: position, end: m.end } };
}

/** Same as pressStart for the end mark, which also needs a fixed start. */
export function pressEnd(m: ExportMarks, position: number): MarkPress {
  if (!canPressEnd(m)) return { marks: m };
  if (m.placing !== "end") return { marks: { ...m, placing: "end" } };
  if (position <= m.start!) return { marks: m, refused: "end_before_start" };
  return { marks: { start: m.start, end: position } };
}

/** Escape: stop placing a mark (keeping the last fixed value); with nothing being placed, clear both marks. */
export function escapeMarks(m: ExportMarks): ExportMarks {
  if (m.placing) return { start: m.start, end: m.end };
  return {};
}
