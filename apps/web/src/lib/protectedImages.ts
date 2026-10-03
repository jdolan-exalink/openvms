const DB_NAME = "openvms-protected-images";
const STORE = "images";

export type ProtectedImage = {
  id: string;
  kind: "alarm" | "plate";
  title: string;
  detail: string;
  comment: string;
  savedAt: string;
  blob: Blob;
  /** Live full frame. Optional on rows saved before this field existed. */
  fullUrl?: string;
  /** Live clip. Plates only; absent when the read has no recording. */
  clipUrl?: string;
};

/** Live full frame and clip for a protected row. Older plate copies are recovered from the id. */
export function protectedMedia(image: Pick<ProtectedImage, "id" | "kind" | "fullUrl" | "clipUrl">): { fullUrl?: string; clipUrl?: string } {
  if (image.fullUrl || image.clipUrl) return { fullUrl: image.fullUrl, clipUrl: image.clipUrl };
  if (image.kind === "plate" && image.id.startsWith("plate:")) {
    const id = encodeURIComponent(image.id.slice("plate:".length));
    return {
      fullUrl: `/media/v1/lpr/reads/${id}/snapshot.jpg`,
      clipUrl: `/media/v1/lpr/reads/${id}/clip.mp4`,
    };
  }
  return {};
}

function openDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 1);
    request.onupgradeneeded = () => {
      const db = request.result;
      if (!db.objectStoreNames.contains(STORE)) db.createObjectStore(STORE, { keyPath: "id" });
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("No se pudo abrir el archivo de imágenes protegidas"));
  });
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("No se pudo leer la imagen protegida"));
  });
}

/** Local copy of a snapshot. Frigate retention can drop the original; this copy stays on this browser. */
export async function listProtectedImages(): Promise<ProtectedImage[]> {
  const db = await openDb();
  try {
    const items = await requestResult(db.transaction(STORE, "readonly").objectStore(STORE).getAll() as IDBRequest<ProtectedImage[]>);
    return items.sort((a, b) => b.savedAt.localeCompare(a.savedAt));
  } finally {
    db.close();
  }
}

export async function saveProtectedImage(image: ProtectedImage): Promise<void> {
  const db = await openDb();
  try {
    const tx = db.transaction(STORE, "readwrite");
    tx.objectStore(STORE).put(image);
    await new Promise<void>((resolve, reject) => {
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error ?? new Error("No se pudo proteger la imagen"));
      tx.onabort = () => reject(tx.error ?? new Error("No se pudo proteger la imagen"));
    });
  } finally {
    db.close();
  }
}

export async function protectRemoteImage(input: { id: string; kind: ProtectedImage["kind"]; title: string; detail: string; comment?: string; imageUrl: string; fullUrl?: string; clipUrl?: string }): Promise<void> {
  const response = await fetch(input.imageUrl, { credentials: "include" });
  if (!response.ok) throw new Error("No se pudo copiar la imagen");
  const blob = await response.blob();
  if (!blob.size) throw new Error("La imagen está vacía");
  await saveProtectedImage({
    id: input.id,
    kind: input.kind,
    title: input.title,
    detail: input.detail,
    comment: input.comment?.trim() ?? "",
    savedAt: new Date().toISOString(),
    blob,
    fullUrl: input.fullUrl,
    clipUrl: input.clipUrl,
  });
}
