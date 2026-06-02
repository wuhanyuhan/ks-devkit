import { existsSync, readFileSync } from "fs";
import { parse } from "yaml";
import { defaultAuthMode, type AuthMode } from "./types";

export interface ManifestInfo {
  id?: string;
  name?: string;
  version?: string;
  authMode?: AuthMode;
}

interface RawManifest {
  id?: string;
  name?: string;
  version?: string;
  mount?: {
    service?: { auth_mode?: string };
    extension?: { auth_mode?: string };
  };
}

/**
 * 加载 manifest.yaml。文件不存在返回 null（非错误，作为 fallback 源是允许缺失的）。
 * 只提取 SDK 实际用到的字段：id / name / version / auth_mode（从 service 或 extension mount）。
 */
export function loadManifest(path: string): ManifestInfo | null {
  if (!existsSync(path)) return null;
  const raw = readFileSync(path, "utf-8");
  const data = parse(raw) as RawManifest;

  const rawAuth =
    data.mount?.service?.auth_mode ?? data.mount?.extension?.auth_mode;

  const info: ManifestInfo = {
    id: data.id,
    name: data.name,
    version: data.version,
  };

  if (rawAuth !== undefined) {
    info.authMode = defaultAuthMode(rawAuth);
  }

  return info;
}
