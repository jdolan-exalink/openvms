import { describe, expect, it } from "vitest";
import { identifyPlan, sanitizePlanSvg, validatePlanDimensions, validatePdfPage } from "./planConversion";
const encode = (value: string) => new TextEncoder().encode(value);
describe("plan conversion bounds", () => {
  it("identifies actual bytes rather than extension or declared MIME", () => {
    expect(identifyPlan(encode("%PDF-1.7\n"))).toBe("pdf");
    expect(identifyPlan(encode('<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10"/>'))).toBe("svg");
    expect(identifyPlan(new Uint8Array([137,80,78,71,13,10,26,10]))).toBe("png");
    expect(() => identifyPlan(encode('<html><svg/></html>'))).toThrow();
    expect(() => identifyPlan(encode('GIF89a'))).toThrow();
  });
  it("rejects invalid and excessive dimensions before allocating pixels", () => {
    expect(() => validatePlanDimensions(8193, 1)).toThrow();
    expect(() => validatePlanDimensions(4096, 4096)).toThrow();
    expect(() => validatePlanDimensions(0, 5)).toThrow();
    expect(() => validatePlanDimensions(NaN, 5)).toThrow();
    expect(validatePlanDimensions(8000, 1000)).toEqual({width:8000,height:1000});
  });
  it("bounds selected PDF pages", () => {
    expect(() => validatePdfPage(101, 1)).toThrow();
    expect(() => validatePdfPage(2, 3)).toThrow();
    expect(() => validatePdfPage(2, 1.5)).toThrow();
    expect(validatePdfPage(2, 2)).toBe(2);
  });
});
describe("restricted SVG backgrounds", () => {
  it("keeps basic shapes and returns explicit warnings for removed active content", async () => {
    const result = await sanitizePlanSvg('<svg xmlns="http://www.w3.org/2000/svg" width="100" height="80"><rect width="100" height="80" fill="#fff"/><script>alert(1)</script><g onclick="evil()"><path d="M0 0L20 20" stroke="red"/></g></svg>');
    expect(result.width).toBe(100);
    expect(result.height).toBe(80);
    expect(result.svg).toContain("<rect");
    expect(result.svg).not.toMatch(/script|onclick/);
    expect(result.warnings.length).toBeGreaterThan(0);
  });
  it("removes external resources, CSS, animation, symbols and URL paints", async () => {
    const result = await sanitizePlanSvg('<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><style>rect{fill:url(https://evil)}</style><foreignObject/><image href="https://evil"/><use href="#x"/><animate/><symbol id="x"/><rect style="fill:red" fill="url(https://evil)" stroke="url(#x)" width="20" height="20"/></svg>');
    expect(result.svg).not.toMatch(/href|https:|url\(|style|foreignObject|image|use|animate|symbol/);
    expect(result.warnings.length).toBeGreaterThan(0);
  });
  it("rejects DTD, entities, malformed XML and huge declaration before sanitation", async () => {
    for (const svg of ['<!DOCTYPE svg [<!ENTITY x "evil">]><svg/>', '<svg><g></svg>', '<svg width="999999" height="10"/>', '<svg viewBox="0 0 10 10"><path d="'+ 'M1 1 '.repeat(50000)+'"/></svg>']) {
      await expect(sanitizePlanSvg(svg)).rejects.toThrow();
    }
  });
  it("uses viewBox dimensions without inventing a background size", async () => {
    const result = await sanitizePlanSvg('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 60"><circle cx="10" cy="10" r="5"/></svg>');
    expect(result.width).toBe(120); expect(result.height).toBe(60);
    await expect(sanitizePlanSvg('<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>')).rejects.toThrow();
  });
});
