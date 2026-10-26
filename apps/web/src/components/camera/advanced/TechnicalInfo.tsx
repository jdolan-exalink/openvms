import { useState } from "react";
import type { PlainError } from "@/lib/plainError";

/** A plain failure message with its technical text collapsed behind "Ver información técnica". */
export function PlainErrorNote({ error }: { error: PlainError }) {
  const [show, setShow] = useState(false);
  return (
    <div role="alert" className="rounded-m3-lg bg-bad/12 px-4 py-3 text-sm text-bad">
      <p>{error.message}</p>
      {error.technical && (
        <>
          <button type="button" className="mt-1 text-xs underline" aria-expanded={show} onClick={() => setShow((s) => !s)}>Ver información técnica</button>
          {show && <pre className="mt-1 whitespace-pre-wrap break-words text-xs">{error.technical}</pre>}
        </>
      )}
    </div>
  );
}
