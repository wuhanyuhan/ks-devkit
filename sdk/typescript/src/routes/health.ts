import type { Hono } from "hono";

export interface HealthCheck {
  name: string;
  check: () => Promise<void>;
}

/**
 * 注册 /healthz 和 /readyz 路由。
 *
 * /readyz 固定返回 {status:"ok"}（conformance §1 要求）
 * /healthz 聚合所有 HealthCheck：任一失败整体 503
 */
export function registerHealthRoutes(app: Hono, checks: HealthCheck[]): void {
  app.get("/readyz", (c) => c.json({ status: "ok" }));

  app.get("/healthz", async (c) => {
    const results: Record<string, string> = {};
    let anyFailed = false;
    for (const { name, check } of checks) {
      try {
        await check();
        results[name] = "ok";
      } catch (err) {
        results[name] = err instanceof Error ? err.message : String(err);
        anyFailed = true;
      }
    }
    if (anyFailed) {
      return c.json({ status: "unhealthy", checks: results }, 503);
    }
    return c.json({ status: "ok" });
  });
}
