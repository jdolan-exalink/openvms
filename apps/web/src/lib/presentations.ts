/**
 * Presentations are the Live grid shapes: a columns×rows lattice whose panes are rectangles.
 * Deleting an internal line merges two panes when the result stays a rectangle; restoring a
 * removed line splits that pane back in two. The catalog lives in localStorage.
 */

export type Pane = { col: number; row: number; colSpan: number; rowSpan: number };

export type Presentation = {
  id: string;
  name: string;
  columns: number;
  rows: number;
  panes: Pane[];
  /** Standards stay first in the menu and cannot be edited or deleted. */
  builtin?: boolean;
};

export const MAX_GRID = 8;

export function divisionName(count: number): string {
  return count === 1 ? "1 división" : `${count} divisiones`;
}

export function uniformPanes(columns: number, rows: number): Pane[] {
  const panes: Pane[] = [];
  for (let row = 0; row < rows; row++) {
    for (let col = 0; col < columns; col++) panes.push({ col, row, colSpan: 1, rowSpan: 1 });
  }
  return panes;
}

export function clampGrid(value: number): number {
  if (!Number.isInteger(value)) return 1;
  return Math.min(MAX_GRID, Math.max(1, value));
}

export function uniformPresentation(columns: number, rows: number, id = "uniform"): Presentation {
  const c = clampGrid(columns);
  const r = clampGrid(rows);
  const panes = uniformPanes(c, r);
  return { id, name: divisionName(panes.length), columns: c, rows: r, panes };
}

function paneAt(panes: Pane[], col: number, row: number): number {
  return panes.findIndex((pane) => col >= pane.col && col < pane.col + pane.colSpan && row >= pane.row && row < pane.row + pane.rowSpan);
}

export type GridSegment = {
  orientation: "v" | "h";
  /** Unit cell on the left of a vertical line, or above a horizontal line. */
  col: number;
  row: number;
  solid: boolean;
};

/** Every internal lattice line, solid where two panes meet and dashed where a line was removed. */
export function gridSegments(columns: number, rows: number, panes: Pane[]): GridSegment[] {
  const segments: GridSegment[] = [];
  for (let row = 0; row < rows; row++) {
    for (let col = 0; col < columns - 1; col++) {
      segments.push({ orientation: "v", col, row, solid: paneAt(panes, col, row) !== paneAt(panes, col + 1, row) });
    }
  }
  for (let row = 0; row < rows - 1; row++) {
    for (let col = 0; col < columns; col++) {
      segments.push({ orientation: "h", col, row, solid: paneAt(panes, col, row) !== paneAt(panes, col, row + 1) });
    }
  }
  return segments;
}

function unionRect(a: Pane, b: Pane): Pane | null {
  const col = Math.min(a.col, b.col);
  const row = Math.min(a.row, b.row);
  const right = Math.max(a.col + a.colSpan, b.col + b.colSpan);
  const bottom = Math.max(a.row + a.rowSpan, b.row + b.rowSpan);
  if ((right - col) * (bottom - row) !== a.colSpan * a.rowSpan + b.colSpan * b.rowSpan) return null;
  return { col, row, colSpan: right - col, rowSpan: bottom - row };
}

function readingOrder(panes: Pane[]): Pane[] {
  return [...panes].sort((a, b) => a.row - b.row || a.col - b.col);
}

/**
 * toggleSegment removes a solid line by merging the panes it separates, or restores a dashed
 * line by splitting that pane along the whole cut. A merge that would leave an L-shape is ignored.
 */
export function toggleSegment(panes: Pane[], segment: GridSegment): Pane[] {
  if (segment.solid) {
    const a = segment.orientation === "v" ? paneAt(panes, segment.col, segment.row) : paneAt(panes, segment.col, segment.row);
    const b = segment.orientation === "v" ? paneAt(panes, segment.col + 1, segment.row) : paneAt(panes, segment.col, segment.row + 1);
    if (a < 0 || b < 0 || a === b) return panes;
    const merged = unionRect(panes[a]!, panes[b]!);
    if (!merged) return panes;
    return readingOrder([...panes.filter((_, index) => index !== a && index !== b), merged]);
  }
  const index = paneAt(panes, segment.col, segment.row);
  if (index < 0) return panes;
  const pane = panes[index]!;
  let parts: [Pane, Pane] | null = null;
  if (segment.orientation === "v") {
    const cut = segment.col + 1;
    if (cut > pane.col && cut < pane.col + pane.colSpan) {
      parts = [
        { col: pane.col, row: pane.row, colSpan: cut - pane.col, rowSpan: pane.rowSpan },
        { col: cut, row: pane.row, colSpan: pane.col + pane.colSpan - cut, rowSpan: pane.rowSpan },
      ];
    }
  } else {
    const cut = segment.row + 1;
    if (cut > pane.row && cut < pane.row + pane.rowSpan) {
      parts = [
        { col: pane.col, row: pane.row, colSpan: pane.colSpan, rowSpan: cut - pane.row },
        { col: pane.col, row: cut, colSpan: pane.colSpan, rowSpan: pane.row + pane.rowSpan - cut },
      ];
    }
  }
  if (!parts) return panes;
  return readingOrder([...panes.filter((_, i) => i !== index), ...parts]);
}

/** panesCover reports whether the panes tile the lattice exactly once. */
export function panesCover(columns: number, rows: number, panes: Pane[]): boolean {
  if (columns < 1 || rows < 1 || panes.length === 0 || panes.length > columns * rows) return false;
  const seen = new Set<string>();
  for (const pane of panes) {
    if (![pane.col, pane.row, pane.colSpan, pane.rowSpan].every((n) => Number.isInteger(n))) return false;
    if (pane.col < 0 || pane.row < 0 || pane.colSpan < 1 || pane.rowSpan < 1) return false;
    if (pane.col + pane.colSpan > columns || pane.row + pane.rowSpan > rows) return false;
    for (let row = pane.row; row < pane.row + pane.rowSpan; row++) {
      for (let col = pane.col; col < pane.col + pane.colSpan; col++) {
        const key = `${col},${row}`;
        if (seen.has(key)) return false;
        seen.add(key);
      }
    }
  }
  return seen.size === columns * rows;
}

export function parsePanes(columns: number, rows: number, value: unknown): Pane[] | null {
  if (!Array.isArray(value)) return null;
  const panes: Pane[] = [];
  for (const item of value) {
    if (!item || typeof item !== "object") return null;
    const pane = item as Partial<Pane>;
    if (typeof pane.col !== "number" || typeof pane.row !== "number" || typeof pane.colSpan !== "number" || typeof pane.rowSpan !== "number") return null;
    panes.push({ col: pane.col, row: pane.row, colSpan: pane.colSpan, rowSpan: pane.rowSpan });
  }
  return panesCover(columns, rows, panes) ? readingOrder(panes) : null;
}

function standardGrid(id: string, columns: number, rows: number, name = columns === 1 && rows === 1 ? "1" : `${columns}×${rows}`): Presentation {
  return { ...uniformPresentation(columns, rows, id), name, builtin: true };
}

function defaultPresentations(): Presentation[] {
  const three = standardGrid("p3", 2, 2, "3");
  three.panes = toggleSegment(three.panes, { orientation: "v", col: 0, row: 1, solid: true });
  return [
    standardGrid("p1", 1, 1),
    three,
    standardGrid("p2x2", 2, 2),
    standardGrid("p3x3", 3, 3),
    standardGrid("p4x4", 4, 4),
    standardGrid("p5x5", 5, 5),
    standardGrid("p6x6", 6, 6),
    standardGrid("p8x8", 8, 8),
  ];
}

export const DEFAULT_PRESENTATIONS: Presentation[] = defaultPresentations();

/** presentationForCount picks the smallest uniform presentation that can hold `count` cameras. */
export function presentationForCount(list: Presentation[], count: number): Presentation {
  const uniform = list.filter((item) => item.panes.length === item.columns * item.rows);
  const pool = (uniform.length ? uniform : list).slice().sort((a, b) => a.panes.length - b.panes.length);
  return pool.find((item) => item.panes.length >= Math.max(1, count)) ?? pool[pool.length - 1]!;
}

export function samePanes(a: Pane[], b: Pane[]): boolean {
  if (a.length !== b.length) return false;
  return a.every((pane, index) => {
    const other = b[index];
    return !!other && pane.col === other.col && pane.row === other.row && pane.colSpan === other.colSpan && pane.rowSpan === other.rowSpan;
  });
}

const CATALOG_KEY = "openvms.live.presentations.v2";
const VIEW_PANES_KEY = "openvms.live.view-panes.v1";

function parsePresentation(value: unknown): Presentation | null {
  if (!value || typeof value !== "object") return null;
  const item = value as Partial<Presentation>;
  if (typeof item.id !== "string" || !item.id) return null;
  if (typeof item.columns !== "number" || typeof item.rows !== "number") return null;
  const columns = clampGrid(item.columns);
  const rows = clampGrid(item.rows);
  const panes = parsePanes(columns, rows, item.panes);
  if (!panes || DEFAULT_PRESENTATIONS.some((standard) => standard.id === item.id)) return null;
  const name = typeof item.name === "string" && item.name.trim() ? item.name.trim() : divisionName(panes.length);
  return { id: item.id, name, columns, rows, panes, builtin: false };
}

/** loadCatalog always leads with the standard shapes, then the personalizadas saved on this browser. */
export function loadCatalog(): Presentation[] {
  try {
    const raw = localStorage.getItem(CATALOG_KEY);
    if (!raw) return DEFAULT_PRESENTATIONS;
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return DEFAULT_PRESENTATIONS;
    const custom = parsed.map(parsePresentation).filter((item): item is Presentation => item !== null);
    return [...DEFAULT_PRESENTATIONS, ...custom];
  } catch {
    return DEFAULT_PRESENTATIONS;
  }
}

export function saveCatalog(list: Presentation[]) {
  try {
    localStorage.setItem(CATALOG_KEY, JSON.stringify(list.filter((item) => !item.builtin)));
  } catch {
    // Private mode: the editor still applies the catalog for this session.
  }
}

type RememberedPanes = { columns: number; rows: number; panes: Pane[] };

function readViewPanes(): Record<string, RememberedPanes> {
  try {
    const parsed = JSON.parse(localStorage.getItem(VIEW_PANES_KEY) ?? "") as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
    return parsed as Record<string, RememberedPanes>;
  } catch {
    return {};
  }
}

/** rememberViewPanes keeps a saved view's custom shape on this browser. The views API stores cells, not spans. */
export function rememberViewPanes(viewId: string, columns: number, rows: number, panes: Pane[]) {
  const all = readViewPanes();
  all[viewId] = { columns, rows, panes };
  try {
    localStorage.setItem(VIEW_PANES_KEY, JSON.stringify(all));
  } catch {
    // The view still opens as a uniform grid.
  }
}

export function recallViewPanes(viewId: string, columns: number, cellCount: number): RememberedPanes | null {
  const stored = readViewPanes()[viewId];
  if (!stored || stored.columns !== columns || stored.panes.length !== cellCount) return null;
  const panes = parsePanes(stored.columns, stored.rows, stored.panes);
  if (!panes) return null;
  return { columns: stored.columns, rows: stored.rows, panes };
}
