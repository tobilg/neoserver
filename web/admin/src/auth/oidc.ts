import type { ConsoleConfig } from "@/api/generated/models";

interface Discovery {
  authorization_endpoint: string;
  token_endpoint: string;
}
const verifierKey = "neoserver.pkce.verifier";
const stateKey = "neoserver.pkce.state";
const nextKey = "neoserver.pkce.next";

function randomValue(bytes = 32) {
  const value = crypto.getRandomValues(new Uint8Array(bytes));
  return btoa(String.fromCharCode(...value))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replaceAll("=", "");
}

async function challenge(verifier: string) {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(verifier),
  );
  return btoa(String.fromCharCode(...new Uint8Array(digest)))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replaceAll("=", "");
}

export async function discovery(issuer: string): Promise<Discovery> {
  const response = await fetch(
    `${issuer.replace(/\/$/, "")}/.well-known/openid-configuration`,
  );
  if (!response.ok)
    throw new Error(
      "The identity provider discovery document could not be loaded.",
    );
  return response.json() as Promise<Discovery>;
}

export async function beginOIDCLogin(config: ConsoleConfig, next: string) {
  const metadata = await discovery(config.auth.oidc.issuer);
  const verifier = randomValue(64);
  const state = randomValue();
  sessionStorage.setItem(verifierKey, verifier);
  sessionStorage.setItem(stateKey, state);
  sessionStorage.setItem(nextKey, next);
  const url = new URL(metadata.authorization_endpoint);
  url.search = new URLSearchParams({
    response_type: "code",
    client_id: config.auth.oidc.client_id,
    redirect_uri: config.auth.oidc.redirect_uri,
    scope: config.auth.oidc.scopes.join(" "),
    state,
    code_challenge: await challenge(verifier),
    code_challenge_method: "S256",
  }).toString();
  location.assign(url);
}

export async function finishOIDCLogin(
  config: ConsoleConfig,
  code: string,
  state: string,
) {
  if (!code || state !== sessionStorage.getItem(stateKey))
    throw new Error("The identity-provider callback state did not match.");
  const verifier = sessionStorage.getItem(verifierKey);
  if (!verifier)
    throw new Error("The PKCE verifier expired. Start sign-in again.");
  const metadata = await discovery(config.auth.oidc.issuer);
  const response = await fetch(metadata.token_endpoint, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "authorization_code",
      code,
      client_id: config.auth.oidc.client_id,
      redirect_uri: config.auth.oidc.redirect_uri,
      code_verifier: verifier,
    }),
  });
  if (!response.ok)
    throw new Error(
      "The identity provider refused the browser token request. Check PKCE and CORS configuration.",
    );
  const tokens = (await response.json()) as { id_token?: string };
  if (!tokens.id_token)
    throw new Error("The identity provider did not return an ID token.");
  const next = sessionStorage.getItem(nextKey) ?? "/";
  sessionStorage.removeItem(verifierKey);
  sessionStorage.removeItem(stateKey);
  sessionStorage.removeItem(nextKey);
  return { idToken: tokens.id_token, next };
}
