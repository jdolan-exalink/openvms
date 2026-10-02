import { useEffect, useRef, useState } from "react";
import { convertPlan, type ConvertedPlan } from "@/lib/maps/planConversion";
import { uploadFloorPlan } from "@/lib/maps/plans";
interface PlanUploadProps { siteId:string; floorId:string; revision:number; onSaved:()=>void }
/** A keyed context cancels conversion/upload and revokes all previews when the map changes. */
export function PlanUpload(props: PlanUploadProps) {
  return <PlanUploadContext key={`${props.siteId}/${props.floorId}/${props.revision}`} {...props}/>;
}
function PlanUploadContext({siteId,floorId,revision,onSaved}:PlanUploadProps) {
  const [file,setFile] = useState<File>();
  const [page,setPage] = useState(1);
  const [preview,setPreview] = useState<(ConvertedPlan & {url:string})>();
  const [busy,setBusy] = useState(false);
  const [error,setError] = useState<string>();
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
      if(!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "No se pudo guardar el fondo.");
    } finally {if(!controller.signal.aborted) setBusy(false);}
  }
  return <section aria-label="Fondo del plano" className="space-y-2 rounded-xl border border-line bg-surface p-3 text-xs shadow-lg">
    <h3 className="font-semibold">Fondo del plano</h3>
    <label className="block">Archivo de plano<input aria-label="Archivo de plano" type="file" accept="image/png,image/svg+xml,application/pdf,.png,.svg,.pdf"
      className="mt-1 block max-w-64" onChange={event=>{
        const next=event.target.files?.[0];if(!next)return;setFile(next);setPage(1);void prepare(next,1);
      }}/></label>
    {file && <p className="break-all text-muted">{file.name}</p>}
    {preview && preview.pageCount>1 && <label className="block">Página PDF<select aria-label="Página PDF" value={page} disabled={busy}
      onChange={event=>{const next=Number(event.target.value);setPage(next);if(file)void prepare(file,next);}}>
      {Array.from({length:preview.pageCount},(_,index)=><option key={index+1} value={index+1}>{index+1}</option>)}
    </select></label>}
    {preview && <><img src={preview.url} alt="Vista previa del plano" className="max-h-40 max-w-64 rounded border border-line"/>
      <p className="text-muted">{preview.width} × {preview.height} px</p>
      {preview.warnings.map(warning=><p key={warning} role="status" className="text-warning">{warning}</p>)}
    </>}
    {error && <p role="alert">{error}</p>}
    {busy && <p role="status">Procesando plano…</p>}
    <p className="text-muted">PNG, SVG o una página PDF; máximo 20 MiB. Revisa la vista previa antes de guardar.</p>
    <div className="flex gap-2"><button type="button" disabled={!preview || busy} onClick={()=>void save()}
      className="rounded bg-accent px-3 py-1 text-white disabled:opacity-40">Guardar fondo</button>
      {busy && <button type="button" onClick={()=>{request.current?.abort();setBusy(false);clearPreview();}}>Cancelar</button>}
    </div>
  </section>;
}
