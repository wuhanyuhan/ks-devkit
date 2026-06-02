import { AuthMode, defaultAuthMode, isValidAuthMode } from "../types";

export interface AuthResolveInput {
  codeMode?: AuthMode;
  manifestMode?: AuthMode;
  env: Record<string, string | undefined>;
}

export interface AuthResolveResult {
  effectiveMode: AuthMode;
  jwksUrl: string;
}

/**
 * 解析 auth_mode 优先级：code option > manifest > env > 默认 none
 *
 * Strict-by-default：当解析结果为 keystone_jwks 但 KEYSTONE_JWKS_URL 空且
 * KS_APP_AUTH_MODE !== "insecure" 时抛错（与 Go/Python SDK 行为一致）。
 *
 * insecure override：KS_APP_AUTH_MODE=insecure 强制降级为 none（本地开发用）。
 */
export function resolveAuth(input: AuthResolveInput): AuthResolveResult {
  const insecure = input.env.KS_APP_AUTH_MODE === "insecure";
  if (insecure) {
    return { effectiveMode: AuthMode.None, jwksUrl: "" };
  }

  let mode: AuthMode = AuthMode.None;
  if (input.codeMode) {
    mode = input.codeMode;
  } else if (input.manifestMode) {
    mode = input.manifestMode;
  } else if (input.env.KS_APP_AUTH_MODE && isValidAuthMode(input.env.KS_APP_AUTH_MODE)) {
    mode = input.env.KS_APP_AUTH_MODE as AuthMode;
  }

  mode = defaultAuthMode(mode);

  const jwksUrl = input.env.KEYSTONE_JWKS_URL ?? "";

  if (mode === AuthMode.KeystoneJWKS && !jwksUrl) {
    throw new Error(
      "ks-app: auth=keystone_jwks but KEYSTONE_JWKS_URL is empty. " +
        "Set KEYSTONE_JWKS_URL or use KS_APP_AUTH_MODE=insecure for local dev."
    );
  }

  return {
    effectiveMode: mode,
    jwksUrl: mode === AuthMode.KeystoneJWKS ? jwksUrl : "",
  };
}
