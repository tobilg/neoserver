import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/AppDialog";
import { registerConfirmHost, type ConfirmRequest } from "@/lib/confirm";

/** Renders confirmations requested through `confirmAction()`, one at a time. */
export function ConfirmHost() {
  const [queue, setQueue] = useState<ConfirmRequest[]>([]);
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(
    () =>
      registerConfirmHost((request) =>
        setQueue((current) => [...current, request]),
      ),
    [],
  );
  const current = queue[0];
  const settle = (confirmed: boolean) => {
    if (!current) return;
    current.resolve(confirmed);
    setQueue((items) => items.slice(1));
  };
  return (
    <Dialog
      open={Boolean(current)}
      onOpenChange={(open) => {
        if (!open) settle(false);
      }}
    >
      {current && (
        <DialogContent
          role="alertdialog"
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            cancel.current?.focus();
          }}
        >
          <DialogHeader>
            <DialogTitle>{current.title}</DialogTitle>
            {current.description && (
              <DialogDescription>{current.description}</DialogDescription>
            )}
          </DialogHeader>
          <DialogFooter>
            <Button
              ref={cancel}
              variant="outline"
              onClick={() => settle(false)}
            >
              {current.cancelLabel ?? "Cancel"}
            </Button>
            <Button
              variant={current.destructive ? "destructive" : "default"}
              onClick={() => settle(true)}
            >
              {current.confirmLabel ?? "Continue"}
            </Button>
          </DialogFooter>
        </DialogContent>
      )}
    </Dialog>
  );
}
