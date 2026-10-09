import { useEffect, useRef, useState } from "react";
import { convertPlan, type ConvertedPlan } from "@/lib/maps/planConversion";
import { ApiError } from "@/api/client";
import { uploadFloorPlan } from "@/lib/maps/plans";
import { Button, Select } from "@/components/ui";
interface PlanUploadProps { siteId:string; floorId:string; revision:number; onSaved:()=>void; onConflict?:()=>void;onDirty?:(dirty:boolean)=>void; source?: Blob }

async function rotatePng(blob: Blob): Promise<{ blob: Blob; width: number; height: number }> {
  const bitmap = await createImageBitmap(blob);
  const canvas = document.createElement("canvas");
  canvas.width = bitmap.height;
  canvas.height = bitmap.width;
  const context = canvas.getContext("2d");
  if (!context) throw new Error("No se pudo girar la imagen.");
  context.translate(canvas.width / 2, canvas.height / 2);
  context.rotate(Math.PI / 2);
  context.drawImage(bitmap, -bitmap.width / 2, -bitmap.height / 2);
  bitmap.close();
  const next = await new Promise<Blob>((resolve, reject) => canvas.toBlob((value) => value ? resolve(value) : reject(new Error("No se pudo girar la imagen.")), "image/png"));
  return { blob: next, width: canvas.width, height: canvas.height };
}
/** A keyed context cancels conversion/upload and revokes all previews when the map changes. */
export function PlanUpload(props: PlanUploadProps) {
  return <PlanUploadContext key={`${props.siteId}/${props.floorId}/${props.revision}`} {...props}/>;
}
function PlanUploadContext({siteId,floorId,revision,onSaved,onConflict,onDirty,source}:PlanUploadProps) {
  const [file,setFile] = useState<File>();
  const [page,setPage] = useState(1);
  const [preview,setPreview] = useState<(ConvertedPlan & {url:string})>();
  const [busy,setBusy] = useState(false);
  const [error,setError] = useState<string>();
  useEffect(()=>{onDirty?.(!!preview||busy);return()=>onDirty?.(false);},[preview,busy,onDirty]);
  const request = useRef<AbortController | null>(null);
  const previewUrl = useRef<string | null>(null);
  useEffect(()=>()=>{
    request.current?.abort();
    if(previewUrl.current) URL.revokeObjectURL(previewUrl.current);
  },[]);
  function clearPreview() {
    if(previewUrl.current) URL.revokeObjectURL(previewUrl.current);
    previewUrl.current=null;setPreview(undefined);
  }
  async function prepare(next:File,selectedPage:number) {
    request.current?.abort(); clearPreview();setError(undefined);setBusy(true);
    const controller = new AbortController();request.current=controller;
    try {
      const result=await convertPlan(next,{page:selectedPage,signal:controller.signal});
      if(controller.signal.aborted || request.current!==controller) return;
      const url=URL.createObjectURL(result.blob);previewUrl.current=url;setPreview({...result,url});
    } catch(cause) {
      if(!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "No se pudo convertir el plano.");
    } finally { if(request.current===controller && !controller.signal.aborted) setBusy(false); }
  }
  async function save() {
    if(!preview || busy) return;
    setBusy(true);setError(undefined);
    const controller=new AbortController();request.current?.abort();request.current=controller;
    try {
      await uploadFloorPlan(siteId,floorId,revision,preview.blob,controller.signal);
      if(!controller.signal.aborted) {clearPreview();onSaved();}
    } catch(cause) {
      if(!controller.signal.aborted) {setError(cause instanceof Error ? cause.message : "No se pudo guardar el fondo.");if(cause instanceof ApiError && cause.status===409)onConflict?.();}
    } finally {if(!controller.signal.aborted) setBusy(false);}
  }
  async function rotate() {
    if (!preview || busy) return;
    setBusy(true); setError(undefined);
    try {
      const turned = await rotatePng(preview.blob);
      if (previewUrl.current) URL.revokeObjectURL(previewUrl.current);
      const url = URL.createObjectURL(turned.blob);
      previewUrl.current = url;
      setPreview({ ...preview, ...turned, url, warnings: preview.warnings });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "No se pudo girar la imagen.");
    } finally { setBusy(false); }
  }
  async function adjustCurrent() {
    if (!source || busy) return;
    setBusy(true); setError(undefined);
    try {
      const bitmap = await createImageBitmap(source);
      if (previewUrl.current) URL.revokeObjectURL(previewUrl.current);
      const url = URL.createObjectURL(source);
      previewUrl.current = url;
      setPreview({ blob: source, url, width: bitmap.width, height: bitmap.height, warnings: [], pageCount: 1 });
      bitmap.close();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "No se pudo abrir la imagen actual.");
    } finally { setBusy(false); }
  }
  return <section aria-label="Fondo del plano" className="space-y-3 rounded-m3-xl bg-surface-1/95 p-4 text-xs shadow-lg backdrop-blur">
    <h3 className="text-lg font-bold">Fondo del plano</h3>
    <label className="m3-press inline-flex h-11 cursor-pointer items-center rounded-full bg-secondary-container px-5 text-sm font-bold text-on-secondary-container has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-primary">
      Elegir imagen
      <input aria-label="Archivo de plano" type="file" accept="image/png,image/svg+xml,application/pdf,.png,.svg,.pdf"
        className="sr-only" onChange={event=>{
          const next=event.target.files?.[0];if(!next)return;setFile(next);setPage(1);void prepare(next,1);
        }}/>
    </label>
    {file && <p className="break-all font-mono text-on-surface-variant">{file.name}</p>}
    {preview && preview.pageCount>1 && <label className="block">Página PDF<Select aria-label="Página PDF" value={page} disabled={busy} className="mt-1"
      onChange={event=>{const next=Number(event.target.value);setPage(next);if(file)void prepare(file,next);}}>
      {Array.from({length:preview.pageCount},(_,index)=><option key={index+1} value={index+1}>{index+1}</option>)}
    </Select></label>}
    {preview && <><img src={preview.url} alt="Vista previa del plano" className="max-h-40 max-w-64 rounded-m3-lg"/>
      <p className="font-mono text-on-surface-variant">{preview.width} × {preview.height} px</p>
      {preview.warnings.map(warning=><p key={warning} role="status" className="text-warn">{warning}</p>)}
      <Button variant="tonal" disabled={busy} onClick={() => void rotate()}>Girar</Button>
    </>}
    {!preview && source && <Button variant="tonal" disabled={busy} onClick={() => void adjustCurrent()}>Acomodar imagen</Button>}
    {error && <p role="alert" className="text-bad">{error}</p>}
    {busy && <p role="status">Procesando plano…</p>}
    <p className="text-on-surface-variant">PNG, SVG o una página PDF; máximo 20 MiB. Girar acomoda el plano antes de guardarlo.</p>
    <div className="flex flex-wrap gap-2"><Button variant="filled" disabled={!preview || busy} onClick={()=>void save()}>Guardar fondo</Button>
      {(busy||preview) && <Button variant="outlined" onClick={()=>{request.current?.abort();setBusy(false);clearPreview();}}>Cancelar</Button>}
    </div>
  </section>;
}
