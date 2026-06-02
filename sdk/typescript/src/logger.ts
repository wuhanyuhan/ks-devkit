/**
 * 极简 JSON logger。用户可通过 createApp({ logger }) 注入自定义实现。
 */

export interface Logger {
  info(msg: string, data?: Record<string, unknown>): void;
  warn(msg: string, data?: Record<string, unknown>): void;
  error(msg: string, data?: Record<string, unknown>): void;
  debug(msg: string, data?: Record<string, unknown>): void;
}

export interface LoggerOptions {
  level?: "debug" | "info" | "warn" | "error";
}

const LEVEL_ORDER = { debug: 0, info: 1, warn: 2, error: 3 } as const;

export function createLogger(opts: LoggerOptions = {}): Logger {
  const threshold = LEVEL_ORDER[opts.level ?? "info"];

  const emit = (level: keyof typeof LEVEL_ORDER, msg: string, data?: Record<string, unknown>) => {
    if (LEVEL_ORDER[level] < threshold) return;
    const line = JSON.stringify({
      level,
      ts: new Date().toISOString(),
      msg,
      ...(data ?? {}),
    });
    console.log(line);
  };

  return {
    debug: (m, d) => emit("debug", m, d),
    info: (m, d) => emit("info", m, d),
    warn: (m, d) => emit("warn", m, d),
    error: (m, d) => emit("error", m, d),
  };
}
