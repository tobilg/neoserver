/**
 * Promise-based confirmation rendered by <ConfirmHost />. Call sites can keep
 * their linear "ask, then act" flow without the browser's native confirm().
 */
export interface ConfirmOptions {
  title: string;
  description?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
}

export interface ConfirmRequest extends ConfirmOptions {
  resolve: (confirmed: boolean) => void;
}

type Listener = (request: ConfirmRequest) => void;
let listener: Listener | undefined;

/** Registers the mounted host. Returns an unsubscribe function. */
export function registerConfirmHost(next: Listener) {
  listener = next;
  return () => {
    if (listener === next) listener = undefined;
  };
}

export function confirmAction(options: ConfirmOptions): Promise<boolean> {
  if (!listener) {
    // Isolated renders (unit tests, embedded screens) have no host.
    const text = [options.title, options.description]
      .filter(Boolean)
      .join("\n\n");
    return Promise.resolve(window.confirm(text));
  }
  const host = listener;
  return new Promise((resolve) => host({ ...options, resolve }));
}

/** The common "unsaved edits" confirmation. */
export function confirmDiscard(title: string, description?: string) {
  return confirmAction({
    title,
    description: description ?? "Your unsaved changes will be lost.",
    confirmLabel: "Discard changes",
    cancelLabel: "Keep editing",
    destructive: true,
  });
}
