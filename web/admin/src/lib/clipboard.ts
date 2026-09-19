import { toast } from "sonner";

export async function copyText(value: string) {
  try {
    await navigator.clipboard.writeText(value);
    toast.success("Copied to clipboard");
  } catch {
    toast.error("Clipboard unavailable. Select and copy the text manually.");
  }
}
