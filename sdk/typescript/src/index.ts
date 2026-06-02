import { App } from "./app";
import type { AppConfig } from "./config";

export function createApp(config: AppConfig): App {
  return new App(config);
}

export { App };
export type { AppConfig } from "./config";
export type { Logger, LoggerOptions } from "./logger";
export type { LLMClient } from "./llm";
export { EmbeddingClient, EmbeddingNotConfiguredError } from "./embedding";
export type { EmbeddingResult } from "./embedding";
export { VectorStoreClient } from "./vector_store";
export type { Point, SearchOptions, SearchResult, Filter } from "./vector_store";
export type { LifecycleHook } from "./lifecycle";
export { createLogger } from "./logger";
export {
  AuthMode,
  isValidAuthMode,
  defaultAuthMode,
  type MetaResponse,
  type ToolInfo,
  type ConfigUIInfo,
  type MetaNavDecl,
  type MetaPermissionDecl,
  type MetaConfigMode,
  type MetaConfigStatus,
} from "./types";
export { fetchKeystoneManagedEnv, SelfClient, KeystoneSelfFetchError } from "./keystone-env";
export type {
  FetchManagedEnvOptions,
  FetchManagedEnvResult,
  SelfClientOptions,
} from "./keystone-env";

// ── Capability Mesh ──────────────────────────────────────────────────────────
export { canonical } from "./canonical";
export {
  DispatcherClient,
  type InvokeOptions,
  type InvokeResult,
  type InvokeSyncResult,
  type InvokeAsyncResult,
  type TaskSnapshot,
} from "./dispatcher_client";
export { CapabilityCall, Task, type TaskInit } from "./task";
export { EventsClient, EventStream, type EventsMode, type Event } from "./events_client";
export { ScopedJWTVerifier, scopedJwtMiddleware, type ScopedClaims } from "./scoped_jwt";
export {
  CapabilityRegistry,
  type CapabilityHandler,
  type CapabilityEntry,
  type GeneratedTool,
  type HttpEndpointRoute,
} from "./capability";
export {
  buildContextFromMeta,
  createCapabilityContext,
  extractCallerContext,
  type CapabilityContext,
  type CapabilityContextInit,
  type ProgressReporter,
  type CallerContext,
} from "./capability_context";
export { parseManifestCapabilities, type ManifestCapabilities } from "./manifest_capabilities";
export type { CapabilitySpec, BackendSpec, RequiresCapability } from "./types";
export {
  KeystoneError,
  AuthError, TokenInvalidError, TokenExpiredError, TokenAudienceMismatchError,
  PermissionError, CapabilityForbiddenError, ApprovalRequiredError, CapabilityDisabledError,
  NotFoundError, CapabilityNotFoundError, TaskNotFoundError,
  ValidationError, InvalidArgsError, ManifestMismatchError,
  DependencyError, CapabilityUnavailableError, LoopDetectedError, GuardrailBlockedError,
  ExecutionError, BackendError, TimeoutError, CancelledError, DispatcherRestartedError,
  RateLimitError, CapabilityConcurrencyLimitError, UserQuotaExceededError, AppQuotaExceededError,
  mapHttpError,
} from "./errors";
