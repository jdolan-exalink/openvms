import { type PointerEvent as ReactPointerEvent, useEffect, useState } from "react";
import { TouchDoubleTap } from "@/lib/touch";

/**
 * useTouchDoubleTap gives an element a touch-only double tap (two taps within 300 ms and 24 px).
 * Spread `handlers` on it; `justHandled()` is true right after one fired, so the element's own
 * onDoubleClick / onClick can ignore the dblclick or click the browser synthesizes afterwards.
 */
export function useTouchDoubleTap(onDoubleTap: () => void) {
  const [tap] = useState(() => new TouchDoubleTap<ReactPointerEvent<HTMLElement>>());
  useEffect(() => {
    tap.listen(onDoubleTap);
  });
  return { handlers: tap.handlers, justHandled: () => tap.justHandled() };
}
