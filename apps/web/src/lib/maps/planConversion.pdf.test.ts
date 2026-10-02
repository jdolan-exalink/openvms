import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { convertPlan, PLAN_TIMEOUT_MS } from "./planConversion";
const mocks=vi.hoisted(()=>({getDocument:vi.fn(),workerOptions:{workerSrc:""}}));
vi.mock("pdfjs-dist",()=>({getDocument:mocks.getDocument,GlobalWorkerOptions:mocks.workerOptions}));
vi.mock("pdfjs-dist/build/pdf.worker.min.mjs?url",()=>({default:"/assets/local-pdf-worker.mjs"}));
const file = () => ({size:9,arrayBuffer:async()=>new TextEncoder().encode('%PDF-1.7\n').buffer}) as File;
beforeEach(()=>{mocks.getDocument.mockReset();});
afterEach(()=>{vi.useRealTimers();vi.restoreAllMocks();});
it("rejects excessive page count and destroys loader without allocating canvas",async()=>{
 const destroy=vi.fn(async()=>{});const getPage=vi.fn();
 mocks.getDocument.mockReturnValue({promise:Promise.resolve({numPages:101,getMetadata:async()=>({info:{EncryptFilterName:null}}),getPage}),destroy});
 await expect(convertPlan(file())).rejects.toThrow('1–100');
 expect(getPage).not.toHaveBeenCalled();expect(destroy).toHaveBeenCalled();
 expect(mocks.workerOptions.workerSrc).toBe('/assets/local-pdf-worker.mjs');
 expect(mocks.getDocument.mock.calls[0]?.[0]).toMatchObject({maxImageSize:16_000_000,cMapUrl:'/assets/pdfjs-6.3.289/cmaps/',standardFontDataUrl:'/assets/pdfjs-6.3.289/standard_fonts/',wasmUrl:'/assets/pdfjs-6.3.289/wasm/'});
});
it("rejects encrypted document without interactive password entry",async()=>{
 let reject!: (reason:Error)=>void;
 const task={promise:new Promise((_,fail)=>{reject=fail;}),destroy:vi.fn(async()=>{reject(new Error('Worker was destroyed'));}),onPassword:undefined as undefined|(()=>void)};
 mocks.getDocument.mockImplementation(()=>{queueMicrotask(()=>task.onPassword?.());return task;});
 await expect(convertPlan(file())).rejects.toThrow('Encrypted PDFs');expect(task.destroy).toHaveBeenCalled();
});
it("destroys pending PDF on cancellation",async()=>{
 let reject!: (reason:Error)=>void;
 const task={promise:new Promise((_,fail)=>{reject=fail;}),destroy:vi.fn(async()=>{reject(new Error('Worker was destroyed'));})};
 mocks.getDocument.mockReturnValue(task);
 const controller=new AbortController();const work=convertPlan(file(),{signal:controller.signal});
 await vi.waitFor(()=>expect(mocks.getDocument).toHaveBeenCalled());controller.abort();
 await expect(work).rejects.toMatchObject({name:'AbortError'});expect(task.destroy).toHaveBeenCalled();
});
it("bounds pending PDF conversion at 15 seconds",async()=>{
 vi.useFakeTimers();let reject!: (reason:Error)=>void;
 mocks.getDocument.mockReturnValue({promise:new Promise((_,fail)=>{reject=fail;}),destroy:vi.fn(async()=>{reject(new Error('destroyed'));})});
 const work=convertPlan(file());const result=expect(work).rejects.toMatchObject({name:'AbortError'});
 await vi.advanceTimersByTimeAsync(PLAN_TIMEOUT_MS);await result;
});
it("rejects oversized viewport before canvas allocation",async()=>{
 const canvas=vi.spyOn(document,'createElement');
 const destroy=vi.fn(async()=>{});
 mocks.getDocument.mockReturnValue({promise:Promise.resolve({numPages:2,getMetadata:async()=>({info:{EncryptFilterName:null}}),getPage:async(page:number)=>{
  expect(page).toBe(2);return {getViewport:()=>({width:9000,height:1})};
 }}),destroy});
 await expect(convertPlan(file(),{page:2})).rejects.toThrow('dimensions');
 expect(canvas).not.toHaveBeenCalledWith('canvas');expect(destroy).toHaveBeenCalled();
});
it("settles cancellation even while input reading is pending",async()=>{
 const controller=new AbortController();
 const stalled={size:10,arrayBuffer:()=>new Promise<ArrayBuffer>(()=>{})} as File;
 const work=convertPlan(stalled,{signal:controller.signal});controller.abort();
 await expect(work).rejects.toMatchObject({name:'AbortError'});
 expect(mocks.getDocument).not.toHaveBeenCalled();
});

it("rejects encrypted PDFs that open with an empty password",async()=>{
 const getPage=vi.fn();const destroy=vi.fn(async()=>{});
 mocks.getDocument.mockReturnValue({promise:Promise.resolve({numPages:1,getMetadata:async()=>({info:{EncryptFilterName:"Standard"}}),getPage}),destroy});
 await expect(convertPlan(file())).rejects.toThrow("Encrypted PDFs");expect(getPage).not.toHaveBeenCalled();
});
