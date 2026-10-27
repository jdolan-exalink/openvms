import { createContext, type ReactNode, useContext, useState } from "react";
import { type CameraConfigChange, useCameraConfigChange } from "@/lib/useCameraConfigChange";
import { ChangeReviewDialog } from "./ChangeReviewDialog";

export type CameraChange = CameraConfigChange & {
  /** Review (and then apply) the normalized draft in the ChangeReviewDialog. */
  openReview: () => void;
};

const Ctx = createContext<CameraChange | null>(null);

/** Owns the normalized-config draft of one camera and the review dialog; tabs use `useCameraChange()`. */
export function CameraChangeProvider({ cameraId, children }: { cameraId: string; children: ReactNode }) {
  const change = useCameraConfigChange(cameraId);
  const [open, setOpen] = useState(false);
  const { review } = change;
  const value: CameraChange = {
    ...change,
    openReview: () => {
      review.reset();
      change.apply.reset();
      setOpen(true);
      review.mutate();
    },
  };
  return (
    <Ctx.Provider value={value}>
      {children}
      {open && <ChangeReviewDialog change={value} onClose={() => setOpen(false)} />}
    </Ctx.Provider>
  );
}

export function useCameraChange(): CameraChange {
  const v = useContext(Ctx);
  if (!v) throw new Error("useCameraChange must be used inside CameraChangeProvider");
  return v;
}

/** Same as useCameraChange but null outside the provider (components shared with other screens). */
export function useOptionalCameraChange(): CameraChange | null {
  return useContext(Ctx);
}
