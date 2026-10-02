import { Blob as NodeBlob } from "node:buffer";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { loadFloorPlan, uploadFloorPlan } from "./plans";
beforeEach(() => vi.stubGlobal("Blob", NodeBlob));
afterEach(() => vi.unstubAllGlobals());
it("loads authenticated scoped plan bytes, never plan_key", async () => {
 const fetch = vi.fn(async (request:Request) => {
  expect(new URL(request.url).pathname).toBe('/api/v1/maps/sites/s/floors/f/plan');
  return new Response(new Blob(['png'],{type:'image/png'}),{headers:{'Content-Type':'image/png'}});
 });
 vi.stubGlobal('fetch',fetch);
 expect((await loadFloorPlan('s','f',new AbortController().signal)).type).toBe('image/png');
});
it("uploads only binary PNG with exact If-Match revision", async () => {
 vi.stubGlobal('fetch', vi.fn(async (request:Request) => {
  expect(request.method).toBe('PUT'); expect(request.headers.get('If-Match')).toBe('7');
  expect(request.headers.get('Content-Type')).toBe('image/png');
  expect(await request.text()).toBe('canonical');
  return Response.json({id:'f',building_id:'b',name:'Ground',ordinal:0,revision:8});
 }));
 expect((await uploadFloorPlan('s','f',7,new Blob(['canonical'],{type:'image/png'}))).revision).toBe(8);
 await expect(uploadFloorPlan('s','f',7,new Blob(['raw'],{type:'image/svg+xml'}))).rejects.toThrow();
});
it("surfaces conflict without blind retry", async () => {
 const fetch=vi.fn(async () => Response.json({code:'revision_conflict',message:'Refresh map revision.'},{status:409}));
 vi.stubGlobal('fetch',fetch);
 await expect(uploadFloorPlan('s','f',7,new Blob(['png'],{type:'image/png'}))).rejects.toThrow('Refresh map revision.');
 expect(fetch).toHaveBeenCalledTimes(1);
});
