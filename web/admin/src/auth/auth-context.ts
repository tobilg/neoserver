/**
 * The context and its hook live apart from `AuthProvider.tsx` so that file
 * exports a component and nothing else. React Fast Refresh can only hot-swap a
 * module whose exports are all components; with the hook alongside the provider,
 * editing either one forced a full reload and dropped the signed-in session.
 */
import { createContext, useContext } from "react";
import type { AuthMe, ConsoleConfig } from "@/api/generated/models";

export type LoginPayload =
  | { method: "password"; username: string; password: string }
  | { method: "token"; token: string }
  | { method: "oidc"; id_token: string };

export type AuthContextValue = {
  config?: ConsoleConfig;
  me?: AuthMe;
  loading: boolean;
  login: (payload: LoginPayload) => Promise<AuthMe>;
  logout: () => Promise<void>;
  refresh: () => Promise<AuthMe>;
};

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error("useAuth must be used inside AuthProvider");
  return value;
}
