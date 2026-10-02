import type { RenderTask } from "pdfjs-dist";

/** Uploaded documents are converted off-DOM; only bounded PNG is sent to the API. */
export const PLAN_MAX_BYTES = 20 * 1024 * 1024;
export const PLAN_MAX_SIDE = 8192;
export const PLAN_MAX_PIXELS = 16_000_000;
export const PLAN_TIMEOUT_MS = 15_000;
const SVG_NS = "http://www.w3.org/2000/svg";
export type PlanKind = "png" | "svg" | "pdf";
export interface ConvertedPlan { blob: Blob; width: number; height: number; warnings: string[]; pageCount: number }

export function validatePlanDimensions(width: number, height: number) {
  width = Math.ceil(width); height = Math.ceil(height);
  if (!Number.isFinite(width) || !Number.isFinite(height) || width < 1 || height < 1 ||
      width > PLAN_MAX_SIDE || height > PLAN_MAX_SIDE || width * height > PLAN_MAX_PIXELS) {
    throw new Error("Plan dimensions must fit 8192 pixels per side and 16 million pixels.");
  }
  return { width: Math.ceil(width), height: Math.ceil(height) };
}
export function validatePdfPage(pageCount: number, page: number) {
  if (!Number.isInteger(pageCount) || pageCount < 1 || pageCount > 100) throw new Error("PDF must contain 1–100 pages.");
  if (!Number.isInteger(page) || page < 1 || page > pageCount) throw new Error("Select an existing PDF page.");
  return page;
}
export function identifyPlan(bytes: Uint8Array): PlanKind {
  if (!bytes.length || bytes.length > PLAN_MAX_BYTES) throw new Error("Choose a non-empty file up to 20 MiB.");
  if ([137,80,78,71,13,10,26,10].every((value, index) => bytes[index] === value)) return "png";
  const start = new TextDecoder().decode(bytes.subarray(0, 1024)).trimStart();
  if (start.startsWith("%PDF-")) return "pdf";
  if (/^(?:<\?xml[^?]*\?>\s*)?(?:<!--[^]*?-->\s*)*<svg(?:\s|>)/i.test(start)) return "svg";
  throw new Error("Unsupported file contents. Choose PNG, SVG or PDF.");
}
const SVG_TAGS = ["svg", "g", "path", "rect", "circle", "ellipse", "line", "polyline", "polygon", "text", "tspan", "title", "desc"];
const SVG_ATTRS = ["xmlns", "width", "height", "viewBox", "preserveAspectRatio", "x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "rx", "ry", "d", "points", "transform", "fill", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin", "fill-rule", "opacity", "fill-opacity", "stroke-opacity", "font-size", "font-family", "font-weight", "text-anchor", "dx", "dy"];
export async function sanitizePlanSvg(source: string) {
  if (source.length > 2 * 1024 * 1024 || /<!DOCTYPE|<!ENTITY/i.test(source)) throw new Error("SVG declarations or size are not supported.");
  const document = new DOMParser().parseFromString(source, "image/svg+xml");
  const root = document.documentElement;
  if (document.querySelector("parsererror") || root.localName !== "svg" || root.namespaceURI !== SVG_NS) throw new Error("Invalid SVG document.");
  const elements = Array.from(root.querySelectorAll("*"));
  if (elements.length > 5000) throw new Error("SVG has too many elements.");
  for (const element of [root, ...elements]) {
    let depth = 0;
    for (let parent = element.parentElement; parent; parent = parent.parentElement) depth++;
    if (depth > 32 || (element.getAttribute("d")?.length ?? 0) > 100_000 || (element.getAttribute("points")?.length ?? 0) > 100_000) throw new Error("SVG complexity exceeds supported limits.");
  }
  const viewBox = root.getAttribute("viewBox")?.trim().split(/[\s,]+/).map(Number);
  const dimension = (name: string, fallback?: number) => {
    const value = root.getAttribute(name);
    if (value == null) return fallback ?? NaN;
    return /^\d+(?:\.\d+)?(?:px)?$/.test(value) ? Number(value.replace(/px$/, "")) : NaN;
  };
  const {width,height} = validatePlanDimensions(dimension("width", viewBox?.[2]), dimension("height", viewBox?.[3]));
  const { default: purifier } = await import("dompurify");
  // USE_PROFILES overrides ALLOWED_TAGS, so it is deliberately absent here.
  const sanitized = purifier.sanitize(source, { NAMESPACE: SVG_NS, ALLOWED_TAGS: SVG_TAGS,
    ALLOWED_ATTR: SVG_ATTRS, ALLOW_DATA_ATTR: false, ALLOW_ARIA_ATTR: false, KEEP_CONTENT: false });
  let removed = purifier.removed.length > 0;
  const safe = new DOMParser().parseFromString(sanitized, "image/svg+xml");
  const safeRoot = safe.documentElement;
  if (safe.querySelector("parsererror") || safeRoot.localName !== "svg") throw new Error("SVG cannot be safely converted.");
  for (const element of [safeRoot, ...Array.from(safeRoot.querySelectorAll("*"))]) {
    for (const attribute of Array.from(element.attributes)) {
      if (/url\s*\(|(?:https?|data|javascript):|@import|[\\]/i.test(attribute.value)) {
        element.removeAttribute(attribute.name); removed = true;
      }
    }
  }
  safeRoot.setAttribute("width", String(width)); safeRoot.setAttribute("height", String(height));
  return { svg: new XMLSerializer().serializeToString(safeRoot), width, height,
    warnings: removed ? ["Unsupported or active SVG content was removed. Check the preview before saving."] : [] };
}
function abortError() { return new DOMException("Plan conversion cancelled.", "AbortError"); }
function checkAbort(signal: AbortSignal) { if (signal.aborted) throw abortError(); }
async function pngFromCanvas(canvas: HTMLCanvasElement, signal: AbortSignal) {
  checkAbort(signal);
  const blob = await new Promise<Blob>((resolve,reject) => canvas.toBlob(value => value ? resolve(value) : reject(new Error("Unable to encode PNG.")), "image/png"));
  checkAbort(signal);
  if (blob.size > PLAN_MAX_BYTES) throw new Error("Converted PNG exceeds 20 MiB.");
  return blob;
}
async function rasterImage(blob: Blob, signal: AbortSignal, dimensions?: {width:number;height:number}) {
  const url = URL.createObjectURL(blob);
  const image = new Image();
  let rejectLoad: ((reason: DOMException) => void) | undefined;
  const cancel = () => { image.src = ""; rejectLoad?.(abortError()); };
  signal.addEventListener("abort", cancel, {once:true});
  try {
    await new Promise<void>((resolve,reject) => {
      image.onload = () => resolve(); image.onerror = () => reject(new Error("Unable to decode plan image."));
      rejectLoad = reject;
      image.src = url;
    });
    checkAbort(signal);
    const {width,height} = validatePlanDimensions(dimensions?.width ?? image.naturalWidth, dimensions?.height ?? image.naturalHeight);
    const canvas = document.createElement("canvas"); canvas.width = width; canvas.height = height;
    try {
      const context = canvas.getContext("2d"); if (!context) throw new Error("Canvas rendering unavailable.");
      context.drawImage(image,0,0,width,height);
      return {blob: await pngFromCanvas(canvas,signal),width,height};
    } finally { canvas.width = canvas.height = 0; }
  } finally { signal.removeEventListener("abort",cancel); image.src=""; URL.revokeObjectURL(url); }
}
async function rasterPdf(bytes: Uint8Array, pageNumber: number, signal: AbortSignal): Promise<ConvertedPlan> {
  const pdf = await import("pdfjs-dist");
  const {default:workerUrl} = await import("pdfjs-dist/build/pdf.worker.min.mjs?url");
  checkAbort(signal); pdf.GlobalWorkerOptions.workerSrc = workerUrl;
  const base = `${import.meta.env.BASE_URL}assets/pdfjs-6.3.289/`;
  const task = pdf.getDocument({data:bytes, cMapUrl:`${base}cmaps/`, cMapPacked:true,
    standardFontDataUrl:`${base}standard_fonts/`, wasmUrl:`${base}wasm/`, iccUrl:`${base}iccs/`,
    maxImageSize:PLAN_MAX_PIXELS, canvasMaxAreaInBytes:PLAN_MAX_PIXELS*4, stopAtErrors:true, enableXfa:false});
  // Do not request passwords or render interactive PDF elements.
  task.onPassword = () => { void task.destroy(); };
  let render: RenderTask | undefined;
  const cancel = () => { render?.cancel(); void task.destroy(); };
  signal.addEventListener("abort",cancel,{once:true});
  try {
    const document = await task.promise;
    checkAbort(signal); validatePdfPage(document.numPages,pageNumber);
    const metadata = await document.getMetadata();
    if ((metadata.info as Record<string, unknown>).EncryptFilterName != null) {
      throw new Error("Encrypted PDFs are not supported. Export an unencrypted page.");
    }
    checkAbort(signal);
    const page = await document.getPage(pageNumber);
    const original = page.getViewport({scale:1});
    validatePlanDimensions(original.width, original.height);
    const scale = Math.min(2, PLAN_MAX_SIDE/original.width, PLAN_MAX_SIDE/original.height, Math.sqrt(PLAN_MAX_PIXELS/(original.width*original.height)));
    const viewport = page.getViewport({scale});
    const {width,height} = validatePlanDimensions(viewport.width,viewport.height);
    const canvas = window.document.createElement("canvas"); canvas.width=width; canvas.height=height;
    try {
      render = page.render({canvas,viewport}); await render.promise; checkAbort(signal);
      return {blob:await pngFromCanvas(canvas,signal),width,height,warnings:[],pageCount:document.numPages};
    } finally { page.cleanup(); canvas.width=canvas.height=0; }
  } catch (error) {
    checkAbort(signal);
    if (error instanceof Error && /password|destroyed/i.test(error.message)) throw new Error("Encrypted PDFs are not supported. Export an unencrypted page.", {cause:error});
    throw error;
  } finally { signal.removeEventListener("abort",cancel); await task.destroy(); }
}
/** Timeout bounds asynchronous work; it is not a sandbox against hostile CPU/memory use. */
export async function convertPlan(file: File, options: {page?:number; signal?:AbortSignal} = {}): Promise<ConvertedPlan> {
  if (file.size > PLAN_MAX_BYTES) throw new Error("Choose a file up to 20 MiB.");
  const controller = new AbortController();
  const cancel = () => controller.abort(); options.signal?.addEventListener("abort",cancel,{once:true});
  if(options.signal?.aborted) controller.abort();
  const timeout = setTimeout(cancel,PLAN_TIMEOUT_MS);
  const cancelled = new Promise<never>((_, reject) => {
    controller.signal.addEventListener("abort", () => reject(abortError()), {once:true});
  });
  const work = async (): Promise<ConvertedPlan> => {
    checkAbort(controller.signal);
    const bytes = new Uint8Array(await file.arrayBuffer()); checkAbort(controller.signal);
    const kind = identifyPlan(bytes);
    if (kind === "pdf") return await rasterPdf(bytes,options.page ?? 1,controller.signal);
    if (kind === "svg") {
      const safe = await sanitizePlanSvg(new TextDecoder("utf-8",{fatal:true}).decode(bytes));
      checkAbort(controller.signal);
      return {...await rasterImage(new Blob([safe.svg],{type:"image/svg+xml"}),controller.signal,safe),warnings:safe.warnings,pageCount:1};
    }
    // Validate IHDR before the browser decoder allocates its pixel buffer.
    if(bytes.length < 24 || new TextDecoder().decode(bytes.subarray(12,16)) !== "IHDR") throw new Error("Invalid PNG header.");
    const header = new DataView(bytes.buffer,bytes.byteOffset,bytes.byteLength);
    validatePlanDimensions(header.getUint32(16),header.getUint32(20));
    return {...await rasterImage(new Blob([bytes],{type:"image/png"}),controller.signal),warnings:[],pageCount:1};
  };
  try { return await Promise.race([work(), cancelled]); }
  finally { clearTimeout(timeout); options.signal?.removeEventListener("abort",cancel); }
}
