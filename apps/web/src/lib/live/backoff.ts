/**
 * reconnectDelay returns the wait before reconnect attempt number `attempt` (0-based):
 * exponential from `baseMs`, capped at `maxMs`, with jitter in [50%, 100%) of the ceiling so
 * many players that lose one server at the same moment do not retry in lockstep.
 * `random` returns a value in [0, 1) and is injectable for tests.
 */
export function reconnectDelay(
  attempt: number,
  random: () => number = Math.random,
  { baseMs = 500, maxMs = 32_000 }: { baseMs?: number; maxMs?: number } = {},
): number {
  const ceiling = Math.min(maxMs, baseMs * 2 ** Math.max(0, attempt));
  return Math.round(ceiling * (0.5 + 0.5 * random()));
}
