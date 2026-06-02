/**
 * Capability Mesh 错误层级。镜像 errors_capability.go 的 sentinel 树
 * 与 Python errors.py 的异常树：Go 的 errors.Is/As 两层在 TS 合并为单一 class 层级，
 * 用 instanceof 判定大类、读字段拿上下文。
 */

export class KeystoneError extends Error {
  constructor(message?: string) {
    super(message);
    this.name = new.target.name;
  }
}

// ── 401 Auth ──────────────────────────────────────────────────────────────────
export class AuthError extends KeystoneError {}
export class TokenInvalidError extends AuthError {}
export class TokenExpiredError extends AuthError {}
export class TokenAudienceMismatchError extends AuthError {}

// ── 403 Permission ──────────────────────────────────────────────────────────
export class PermissionError extends KeystoneError {}
export class CapabilityForbiddenError extends PermissionError {}
export class ApprovalRequiredError extends PermissionError {}
export class CapabilityDisabledError extends PermissionError {}

// ── 404 NotFound ──────────────────────────────────────────────────────────────
export class NotFoundError extends KeystoneError {}
export class CapabilityNotFoundError extends NotFoundError {
  constructor(public readonly canonicalName: string, message?: string) {
    super(message ?? `capability not found: ${canonicalName}`);
  }
}
export class TaskNotFoundError extends NotFoundError {
  constructor(public readonly taskId: string, message?: string) {
    super(message ?? `task not found: ${taskId}`);
  }
}

// ── 400 Validation ────────────────────────────────────────────────────────────
export class ValidationError extends KeystoneError {}
export class InvalidArgsError extends ValidationError {}
export class ManifestMismatchError extends ValidationError {
  constructor(public readonly registered: string, public readonly manifestNames: string[]) {
    super(
      `capability ${JSON.stringify(registered)} not in manifest.provides.capabilities; ` +
        `manifest_names=${JSON.stringify(manifestNames)}`,
    );
  }
}

// ── Dependency（503 / 508 / 451）────────────────────────────────────────────────
export class DependencyError extends KeystoneError {}
export class CapabilityUnavailableError extends DependencyError {
  constructor(message: string, public readonly retryAfterMs = 0) {
    super(message);
  }
}
export class LoopDetectedError extends DependencyError {}
export class GuardrailBlockedError extends DependencyError {}

// ── Execution ─────────────────────────────────────────────────────────────────
export class ExecutionError extends KeystoneError {}
export class BackendError extends ExecutionError {}
export class TimeoutError extends ExecutionError {
  readonly deadlineMs: number;
  readonly elapsedMs: number;
  constructor(message?: string, opts: { deadlineMs?: number; elapsedMs?: number } = {}) {
    super(
      message ??
        `capability timeout: elapsed=${opts.elapsedMs ?? 0}ms deadline=${opts.deadlineMs ?? 0}ms`,
    );
    this.deadlineMs = opts.deadlineMs ?? 0;
    this.elapsedMs = opts.elapsedMs ?? 0;
  }
}
export class CancelledError extends ExecutionError {}
export class DispatcherRestartedError extends ExecutionError {}

// ── 429 RateLimit ─────────────────────────────────────────────────────────────
export class RateLimitError extends KeystoneError {
  constructor(message: string, public readonly retryAfterMs = 0) {
    super(message);
  }
}
export class CapabilityConcurrencyLimitError extends RateLimitError {}
export class UserQuotaExceededError extends RateLimitError {}
export class AppQuotaExceededError extends RateLimitError {}

function retryAfterMs(headers: Headers): number {
  const ra = headers.get("Retry-After");
  if (!ra) return 0;
  const s = parseFloat(ra);
  return Number.isFinite(s) ? Math.round(s * 1000) : 0;
}

/**
 * mapHttpError 把 keystone 标准错误响应映射成错误类（逐条镜像 dispatcher_client.go）。
 * 404 默认返 CapabilityNotFound；GetTask/CancelTask caller 自行重映射为 TaskNotFound。
 */
export function mapHttpError(status: number, body: string, headers: Headers, hint = ""): KeystoneError {
  let code = 0;
  let message = "";
  try {
    const env = JSON.parse(body) as { code?: number; message?: string };
    code = env.code ?? 0;
    message = env.message ?? "";
  } catch {
    // 非 JSON body：保留空 message
  }
  const retry = retryAfterMs(headers);
  switch (status) {
    case 400:
      return new InvalidArgsError(message);
    case 401:
      if (code === 40103) return new TokenAudienceMismatchError(message);
      if (code === 40102) return new TokenExpiredError(message);
      return new TokenInvalidError(message);
    case 403:
      if (code === 40301) return new CapabilityDisabledError(message);
      return new CapabilityForbiddenError(message);
    case 404:
      return new CapabilityNotFoundError(hint);
    case 408:
      return new TimeoutError(message || undefined);
    case 429:
      return new RateLimitError(message, retry);
    case 451:
      return new GuardrailBlockedError(message);
    case 502:
      return new BackendError(message);
    case 503:
      return new CapabilityUnavailableError(message, retry);
    case 508:
      return new LoopDetectedError(message);
    default:
      return new BackendError(`unexpected http status=${status} body=${body}`);
  }
}
