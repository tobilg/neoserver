import type { ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "@/auth/AuthProvider";
import { queryClient } from "@/api/queryClient";
import { Toaster } from "@/components/ui/sonner";
import { ConfirmHost } from "@/components/ConfirmHost";

export function Providers({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>{children}</AuthProvider>
      <Toaster position="bottom-right" closeButton />
      <ConfirmHost />
    </QueryClientProvider>
  );
}
