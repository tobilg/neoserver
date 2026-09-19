import { assertCurrentSession, sessionGeneration } from "./session";

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public detail?: string,
    public code?: number | string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export function runtimeBase() {
  const match = window.location.pathname.match(/^(.*?)\/admin(?:\/|$)/);
  return (match?.[1] ?? "").replace(/\/$/, "");
}

export const basePath = runtimeBase();
export const apiBase = `${basePath}/api/v1`;

export function csrfToken() {
  const cookie = document.cookie
    .split("; ")
    .find((value) => value.startsWith("neosrv_csrf="));
  return cookie
    ? decodeURIComponent(cookie.slice(cookie.indexOf("=") + 1))
    : "";
}

export type ApiFetchOptions = RequestInit & { raw?: boolean };

export async function apiFetch<T>(
  path: string,
  options: ApiFetchOptions = {},
): Promise<T> {
  const generation = sessionGeneration();
  const publicRequest = path === "/console/config";
  const checkSession = () => {
    if (!publicRequest) assertCurrentSession(generation);
  };
  const headers = new Headers(options.headers);
  const body = options.body;
  if (body && !(body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const method = (options.method ?? "GET").toUpperCase();
  if (!["GET", "HEAD", "OPTIONS", "TRACE"].includes(method)) {
    const csrf = csrfToken();
    if (csrf) headers.set("X-CSRF-Token", csrf);
  }
  const response = await fetch(
    path.startsWith("http") ? path : `${apiBase}${path}`,
    {
      ...options,
      headers,
      credentials: "same-origin",
    },
  );
  checkSession();
  if (!response.ok) {
    let problem: {
      code?: number | string;
      message?: string;
      detail?: string;
      error?: string;
    } = {};
    const errorBody = await response.text();
    checkSession();
    try {
      problem = JSON.parse(errorBody) as typeof problem;
    } catch {
      problem.message = errorBody;
    }
    if (response.status === 401 && !location.pathname.endsWith("/login")) {
      window.dispatchEvent(new CustomEvent("neoserver:unauthorized"));
    }
    throw new ApiError(
      response.status,
      problem.message ?? problem.error ?? response.statusText,
      problem.detail,
      problem.code,
    );
  }
  if (response.status === 204) return undefined as T;
  if (options.raw) return response as T;
  const result = (await response.json()) as T;
  checkSession();
  return result;
}

export function jsonBody(value: unknown): string {
  return JSON.stringify(value);
}
