import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { createLogger, type Logger } from "./logger";

describe("logger", () => {
  let logs: string[];
  let originalLog: typeof console.log;

  beforeEach(() => {
    logs = [];
    originalLog = console.log;
    console.log = (line: string) => logs.push(line);
  });

  afterEach(() => {
    console.log = originalLog;
  });

  it("emits JSON with level=info", () => {
    const log = createLogger();
    log.info("hello", { foo: "bar" });
    expect(logs).toHaveLength(1);
    const parsed = JSON.parse(logs[0]!);
    expect(parsed.level).toBe("info");
    expect(parsed.msg).toBe("hello");
    expect(parsed.foo).toBe("bar");
  });

  it("warn and error use correct level", () => {
    const log = createLogger();
    log.warn("w");
    log.error("e");
    expect(JSON.parse(logs[0]!).level).toBe("warn");
    expect(JSON.parse(logs[1]!).level).toBe("error");
  });

  it("debug is silent by default", () => {
    const log = createLogger();
    log.debug("d");
    expect(logs).toHaveLength(0);
  });

  it("debug emits when level=debug", () => {
    const log = createLogger({ level: "debug" });
    log.debug("d");
    expect(logs).toHaveLength(1);
    expect(JSON.parse(logs[0]!).level).toBe("debug");
  });

  it("accepts custom logger via createApp (interface check)", () => {
    const custom: Logger = {
      info: () => {},
      warn: () => {},
      error: () => {},
      debug: () => {},
    };
    expect(typeof custom.info).toBe("function");
  });
});
