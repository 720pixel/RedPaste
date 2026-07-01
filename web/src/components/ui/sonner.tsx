import { Toaster as Sonner } from "sonner";

// Toasts follow the app theme (which lives on data-theme).
export function Toaster() {
  const theme = (document.documentElement.getAttribute("data-theme") as "light" | "dark") ?? "light";
  return (
    <Sonner
      theme={theme}
      position="bottom-center"
      toastOptions={{
        classNames: {
          toast: "!bg-popover !text-popover-foreground !border-border !rounded-md !font-sans",
          description: "!text-muted-foreground",
        },
      }}
    />
  );
}
