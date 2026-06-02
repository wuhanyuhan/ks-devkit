import type { Logger } from "./logger";

export type LifecycleHook = () => Promise<void>;

export interface Lifecycle {
  onStartup(fn: LifecycleHook): void;
  onShutdown(fn: LifecycleHook): void;
  runStartup(): Promise<void>;
  runShutdown(): Promise<void>;
}

export function createLifecycle(logger?: Logger): Lifecycle {
  const startupFns: LifecycleHook[] = [];
  const shutdownFns: LifecycleHook[] = [];

  return {
    onStartup: (fn) => { startupFns.push(fn); },
    onShutdown: (fn) => { shutdownFns.push(fn); },

    async runStartup() {
      for (const fn of startupFns) {
        await fn();
      }
    },

    async runShutdown() {
      // 逆序执行（资源释放语义：后注册先释放）
      for (let i = shutdownFns.length - 1; i >= 0; i--) {
        try {
          await shutdownFns[i]!();
        } catch (err) {
          logger?.error("shutdown hook failed", {
            index: i,
            error: err instanceof Error ? err.message : String(err),
          });
        }
      }
    },
  };
}
