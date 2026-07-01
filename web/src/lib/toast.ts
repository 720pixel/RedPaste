import { toast as sonnerToast } from "sonner";

// Thin wrapper so call sites keep the simple toast(message, kind) shape.
export function toast(message: string, kind: "info" | "success" | "error" = "info") {
  if (kind === "success") sonnerToast.success(message);
  else if (kind === "error") sonnerToast.error(message);
  else sonnerToast(message);
}
