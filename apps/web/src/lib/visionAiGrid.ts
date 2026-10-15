export interface GridShape {
  cols: number;
  rows: number;
}

/** Largest dynamic grid: matches the 16-slot layout requested from the API. */
const MAX_CELLS = 16;

/**
 * Grid for `n` active cameras in the dynamic (birdseye-style) Vision AI mode. It grows with the
 * count and never exceeds 4x4. Columns are filled first so tiles stay wide.
 */
export function dynamicGridShape(n: number): GridShape {
  if (!Number.isFinite(n) || n <= 0) return { cols: 0, rows: 0 };
  const count = Math.min(Math.floor(n), MAX_CELLS);
  if (count === 1) return { cols: 1, rows: 1 };
  if (count === 2) return { cols: 2, rows: 1 };
  if (count <= 4) return { cols: 2, rows: 2 };
  if (count <= 6) return { cols: 3, rows: 2 };
  if (count <= 9) return { cols: 3, rows: 3 };
  if (count <= 12) return { cols: 4, rows: 3 };
  return { cols: 4, rows: 4 };
}
