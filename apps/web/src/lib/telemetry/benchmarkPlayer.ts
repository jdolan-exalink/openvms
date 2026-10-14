import { BENCHMARK_CLIP, type BenchmarkDeps, type BenchmarkPlayer, type PlayerStats } from "./benchmark";
import { createFrameSampler } from "./frameSampler";

/**
 * Browser implementation of the benchmark's player: a muted `<video>` fed through MediaSource
 * (or ManagedMediaSource on Safari) with the whole sample clip appended into one SourceBuffer,
 * looped by seeking back on `ended`. Tiles are small but really on screen, so the browser has
 * to decode and composite them like the videowall does.
 */

export const TILE_WIDTH = 160;
export const TILE_HEIGHT = 90;
/** A player that has not appended the clip by then counts as failed instead of hanging the run. */
export const DEFAULT_START_TIMEOUT_MS = 3000;

export type MsePlayerOptions = {
  mimeType?: string;
  tileWidth?: number;
  tileHeight?: number;
  startTimeoutMs?: number;
};

type MediaSourceCtor = typeof MediaSource;

function mediaSourceCtor(): MediaSourceCtor | undefined {
  return window.MediaSource ?? (window as unknown as { ManagedMediaSource?: MediaSourceCtor }).ManagedMediaSource;
}

export function createMsePlayer(container: HTMLElement, clip: ArrayBuffer, options: MsePlayerOptions = {}): BenchmarkPlayer {
  const mimeType = options.mimeType ?? BENCHMARK_CLIP.mimeType;
  const startTimeoutMs = options.startTimeoutMs ?? DEFAULT_START_TIMEOUT_MS;
  const video = container.ownerDocument.createElement("video");
  video.muted = true;
  video.playsInline = true;
  video.preload = "auto";
  video.width = options.tileWidth ?? TILE_WIDTH;
  video.height = options.tileHeight ?? TILE_HEIGHT;
  // ManagedMediaSource refuses to open while remote playback is possible.
  video.disableRemotePlayback = true;
  video.setAttribute("aria-hidden", "true");
  video.style.display = "block";
  video.style.background = "#000";
  container.append(video);

  let errors = 0;
  let stalls = 0;
  let playing = false;
  let disposed = false;
  let objectUrl: string | undefined;
  let settle: (() => void) | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let source: MediaSource | undefined;

  const play = () => {
    try {
      void video.play()?.catch(() => undefined);
    } catch {
      // Autoplay refusals show up as zero decoded frames, which the benchmark reports.
    }
  };
  const onPlaying = () => void (playing = true);
  const onWaiting = () => {
    if (playing) stalls += 1;
  };
  const onError = () => void (errors += 1);
  const onEnded = () => {
    video.currentTime = 0;
    play();
  };
  video.addEventListener("playing", onPlaying);
  video.addEventListener("waiting", onWaiting);
  video.addEventListener("error", onError);
  video.addEventListener("ended", onEnded);

  const finish = (failed: boolean) => {
    if (failed && !disposed) errors += 1;
    clearTimeout(timer);
    const resolve = settle;
    settle = undefined;
    resolve?.();
  };

  return {
    start() {
      if (disposed) return Promise.resolve();
      const MS = mediaSourceCtor();
      if (!MS) {
        errors += 1;
        return Promise.resolve();
      }
      return new Promise<void>((resolve) => {
        settle = resolve;
        timer = setTimeout(() => finish(true), startTimeoutMs);
        try {
          source = new MS();
          source.addEventListener(
            "sourceopen",
            () => {
              if (disposed || !source) return;
              try {
                const buffer = source.addSourceBuffer(mimeType);
                buffer.mode = "segments";
                buffer.addEventListener(
                  "updateend",
                  () => {
                    try {
                      if (source && source.readyState === "open") source.endOfStream();
                    } catch {
                      // The clip is fully appended; a late endOfStream failure does not matter.
                    }
                    play();
                    finish(false);
                  },
                  { once: true },
                );
                buffer.addEventListener("error", onError);
                buffer.appendBuffer(clip);
              } catch {
                finish(true);
              }
            },
            { once: true },
          );
          objectUrl = URL.createObjectURL(source);
          video.src = objectUrl;
        } catch {
          finish(true);
        }
      });
    },
    stats(): PlayerStats {
      let decodedFrames: number | undefined;
      let droppedFrames: number | undefined;
      try {
        const q = typeof video.getVideoPlaybackQuality === "function" ? video.getVideoPlaybackQuality() : undefined;
        if (q) {
          decodedFrames = q.totalVideoFrames;
          droppedFrames = q.droppedVideoFrames;
        }
      } catch {
        // A torn-down element has no playback quality.
      }
      return { decodedFrames, droppedFrames, stalls, errors };
    },
    dispose() {
      if (disposed) return;
      disposed = true;
      video.removeEventListener("playing", onPlaying);
      video.removeEventListener("waiting", onWaiting);
      video.removeEventListener("error", onError);
      video.removeEventListener("ended", onEnded);
      finish(false);
      try {
        video.pause();
      } catch {
        // Already stopped.
      }
      video.removeAttribute("src");
      try {
        video.load();
      } catch {
        // Releases the decoder in browsers that implement it.
      }
      if (objectUrl) URL.revokeObjectURL(objectUrl);
      objectUrl = undefined;
      source = undefined;
      video.remove();
    },
  };
}

/** Real browser wiring: fetch for the clip, requestAnimationFrame for the sampler, timers for waits. */
export function createBrowserBenchmarkDeps(container: HTMLElement): BenchmarkDeps {
  return {
    container,
    async loadClip() {
      const response = await fetch(BENCHMARK_CLIP.url, { cache: "force-cache" });
      if (!response.ok) throw new Error(`benchmark clip request failed with ${response.status}`);
      return response.arrayBuffer();
    },
    createPlayer: (c, clip) => createMsePlayer(c, clip),
    sampler: createFrameSampler((cb) => window.requestAnimationFrame(cb), (h) => window.cancelAnimationFrame(h), () => performance.now()),
    now: () => performance.now(),
    sleep(ms, signal) {
      return new Promise<void>((resolve) => {
        if (signal?.aborted) return resolve();
        const done = () => {
          clearTimeout(timer);
          signal?.removeEventListener("abort", done);
          resolve();
        };
        const timer = setTimeout(done, ms);
        signal?.addEventListener("abort", done, { once: true });
      });
    },
    isHidden: () => document.hidden,
  };
}
