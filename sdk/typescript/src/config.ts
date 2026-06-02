import type { AuthMode } from "./types";
import type { Logger } from "./logger";

export interface AppConfig {
  id: string;
  version?: string;
  auth?: AuthMode;
  manifestPath?: string;
  port?: number;
  host?: string;
  mcpPoolSize?: number;
  logger?: Logger;
  // 未来扩展保留
}

export interface ResolvedConfig extends Required<Omit<AppConfig, "logger" | "auth">> {
  auth?: AuthMode;
  logger: Logger | undefined;
}

export function resolveConfig(c: AppConfig, envPort?: string): ResolvedConfig {
  const parsedPort = envPort ? Number(envPort) : undefined;
  return {
    id: c.id,
    version: c.version ?? "0.1.0",
    auth: c.auth,
    manifestPath: c.manifestPath ?? "manifest.yaml",
    port: c.port ?? parsedPort ?? 8080,
    host: c.host ?? "0.0.0.0",
    mcpPoolSize: c.mcpPoolSize ?? 5,
    logger: c.logger,
  };
}
