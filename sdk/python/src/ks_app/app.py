"""App 类：Keystone MCP Service 应用实例。"""
from __future__ import annotations

import inspect
import logging
import os
import threading
from contextlib import asynccontextmanager
from typing import TYPE_CHECKING, Awaitable, Callable, Optional

if TYPE_CHECKING:
    from .tool_ui import ToolUIBinding

import uvicorn
from starlette.applications import Starlette
from starlette.middleware import Middleware
from starlette.routing import Mount, Route
from starlette.staticfiles import StaticFiles

from .auth.jwks_verifier import JWKSVerifier
from .auth.middleware import JWKSAuthMiddleware
from .auth_resolver import resolve_auth
from .capability import CapabilityEntry, make_capability_decorator
from .config import load_config
from .errors import ManifestMismatch
from .health import health_routes
from .keystone_client import SelfClient
from .embedding import EmbeddingClient
from .llm import LLMClient
from .manifest import ProvidesCapability, load_manifest_capabilities, load_manifest_config_ui
from .config_consistency import (
    NAV_ABSENT,
    NAV_INVALID,
    NAV_VALID,
    check_nav_config_consistency,
)
from .mcpproto import legacy_call_route, legacy_list_route, mcp_route
from .vector_store import VectorStoreClient

logger = logging.getLogger(__name__)


class _SPAStaticFiles(StaticFiles):
    """StaticFiles 的 SPA 变体：未命中文件时 fallback 到 index.html。

    对齐 Go SDK spaStaticHandler 的语义（mount_static_root 用）。
    原生 StaticFiles(html=True) 仅在目录 URL 查 index.html、未命中只查 404.html；
    SPA 场景下 `/users/42` 这类前端路由需要返回 index.html 让 router 接管。
    """

    async def get_response(self, path, scope):  # type: ignore[override]
        from starlette.exceptions import HTTPException

        try:
            return await super().get_response(path, scope)
        except HTTPException as exc:
            if exc.status_code == 404:
                return await super().get_response("index.html", scope)
            raise


class App:
    """Keystone MCP Service 应用。

    Args:
        id: 应用唯一标识（如 "my-app"）
        keystone_auth: 是否启用 keystone_jwks 鉴权（默认 False）
        version: 版本号（反映到 /meta）。默认 "0.1.0"
        manifest_path: manifest.yaml 路径，auth fallback 来源。默认 "manifest.yaml"
    """

    def __init__(
        self,
        id: str,
        *,
        keystone_auth: bool = False,
        version: str = "0.1.0",
        manifest_path: str = "manifest.yaml",
    ):
        self.app_id = id
        self.version = version
        self.manifest_path = manifest_path
        self._code_mode = "keystone_jwks" if keystone_auth else None
        self._tools: dict[str, dict] = {}
        self._config = load_config()
        # 应用启动期一次性从 keystone 拉托管资源凭证，注入 os.environ
        # （仅 KS_APP_TOKEN + KS_GATEWAY_URL 都存在时；失败 warn 不 raise）
        self._maybe_fetch_keystone_managed_env()
        self._llm = LLMClient()
        self._embedding = EmbeddingClient()
        self._health_checks: list[tuple[str, callable]] = []
        self._middlewares: list = []
        self._custom_routes: list[Route] = []
        self._config_ui_dir: Optional[str] = None
        self._config_ui_path: str = "/config-ui"
        # 业务前端 dist 挂到 MCP 根 "/"，
        # 仅 config_mode='none' + open_mode='fullpage' 场景使用。
        # 与 mount_config_ui 互斥（同一路径空间不能既挂 /config-ui 又挂 /）。
        self._static_root_dir: Optional[str] = None
        self._startup_fns: list[Callable[[], Awaitable[None]]] = []
        self._shutdown_fns: list[Callable[[], Awaitable[None]]] = []
        # v0.4.0 新增：声明式 meta 字段（对齐 ks-types v0.5.0 MetaResponse）
        self._nav: dict | None = None
        self._permissions: list[dict] = []
        self._config_mode: str | None = None
        self._protocol_version: str | None = None
        self._config_status: str | None = None
        # Config handle 注册表（供 new_config 写入、
        # 端点 handler 遍历）。包私有字段，外部禁止直接访问。
        self._config_handles: list = []
        self._config_handle_types: set[str] = set()
        # keystore 懒加载 + 并发锁。
        # /config-pubkey / /ks-config/{save,validate} 按需触发加载 + 缓存。
        self._keystore: object | None = None
        self._keystore_lock = threading.Lock()
        # Capability Mesh：capability 注册表 + decorator 工厂。
        # 与 self._tools 并列存放；create_app() 启动期与 manifest.provides 对齐校验。
        self._capabilities: dict[str, CapabilityEntry] = {}
        self.capability = make_capability_decorator(self._capabilities, self.app_id)
        # 测试钩子：注入 ScopedJWTVerifier 静态 key（绕过 JWKS 网络）。
        self._scoped_jwt_test_keys: dict[str, str] = {}
        self._http_capability_path_map: dict[str, str] = {}
        self._scoped_verifier = None
        # DispatcherClient lazy 构造，需 KS_APP_TOKEN + KS_GATEWAY_URL；
        # capability handler 通过 CapabilityContext.dispatcher_client 调 progress / dispatcher
        # （callee 路径）；call_capability caller-side 也用同一 client。
        self._dispatcher_client = None
        # EventsClient lazy 构造（首次 submit 时触发）；mode 'ws' / 'polling'。
        self._events_client = None
        self._events_mode: str = "ws"

    def _maybe_fetch_keystone_managed_env(self) -> None:
        """启动时一次性从 keystone 拉托管资源凭证并堆到 os.environ。

        触发条件：KS_APP_TOKEN + KS_GATEWAY_URL 同时非空字符串。
        任一缺失则跳过——生产容器化场景没有 KS_APP_TOKEN，走 runtime.env 路径。

        注入策略：仅当 key 不在 os.environ 时写入（等价 setdefault），
        本地 .env.local 手填值优先于 keystone 拉取值，调试友好。

        失败不 raise：网络/HTTP/解析任何错误都只打 warn，让 pydantic-settings
        在校验必填字段时报更具体的错。
        """
        token = os.environ.get("KS_APP_TOKEN")
        gateway = os.environ.get("KS_GATEWAY_URL")
        if not token or not gateway:
            return

        try:
            env = SelfClient(gateway, token).fetch_env()
        except Exception as e:
            # 宽口径 catch：KeystoneSelfFetchError + 任何意外错都不能让 App init 挂
            logger.warning("ks-app: fetch keystone managed env failed: %s", e)
            return

        injected = 0
        for k, v in env.items():
            if k not in os.environ:
                os.environ[k] = v
                injected += 1
        if injected:
            logger.info("ks-app: injected %d env vars from keystone", injected)

    def llm(self) -> LLMClient:
        """返回 Keystone LLM Relay 客户端（已在 __init__ 中预初始化）。"""
        return self._llm

    @property
    def embedding(self) -> EmbeddingClient:
        """返回 Keystone 托管 embedding 客户端。"""
        return self._embedding

    def vector_store(self, collection: str) -> VectorStoreClient:
        """返回指定业务 collection 的向量库客户端。"""
        return VectorStoreClient(self._embedding, collection)

    def tool(
        self,
        name: str,
        description: str,
        input_schema: Optional[dict] = None,
        *,
        ui_binding: Optional["ToolUIBinding"] = None,
    ):
        """注册 MCP 工具装饰器。handler 必须是 async 函数。

        Args:
            name: tool 名称（MCP tools/list 的 name 字段）
            description: tool 描述
            input_schema: tool 入参 JSON Schema（可选）
            ui_binding: widget 绑定（widgets-protocol-v1 v0.6.0 新增，
                关键字参数）。声明后会被 /meta 端点序列化为
                ``tools[]._meta.ui``，并自动启用 ``capabilities.ui.enabled``。
        """
        def decorator(func):
            if not inspect.iscoroutinefunction(func):
                raise TypeError(
                    f"tool {name!r} 的 handler 必须是 async 函数，"
                    f"收到的是同步函数 {func.__qualname__}"
                )
            if name in self._tools:
                raise ValueError(f"tool {name!r} 已经注册过了，禁止重复注册")
            self._tools[name] = {
                "handler": func,
                "description": description,
                "input_schema": input_schema,
                "ui_binding": ui_binding,
            }
            return func
        return decorator

    def handle(self, path: str, endpoint, methods: list[str] | None = None):
        """注册自定义路由。返回 self 支持链式调用。"""
        self._custom_routes.append(Route(path, endpoint, methods=methods))
        return self

    def use(self, middleware_cls, **kwargs):
        """注册 Starlette 中间件。用法: app.use(CORSMiddleware, allow_origins=["*"])"""
        self._middlewares.append((middleware_cls, kwargs))
        return self

    def health_check(self, name: str, check_fn):
        """注册自定义健康检查项，check_fn 抛异常表示不健康。"""
        self._health_checks.append((name, check_fn))
        return self

    def mount_config_ui(self, directory: str, path: str = "/config-ui"):
        """挂载前端静态资源到 path（默认 /config-ui）。

        /meta 的 config_ui 字段会自动归一为 ks-types 标准 {"enabled": True, "url": path + "/"}（A5）。

        Raises:
            RuntimeError: 已调过 mount_static_root（两者共用
                          路径空间，互斥）。
        """
        if self._static_root_dir is not None:
            raise RuntimeError(
                "mount_config_ui 与 mount_static_root 互斥，已调过后者"
            )
        self._config_ui_dir = directory
        self._config_ui_path = path
        return self

    def mount_static_root(self, directory: str) -> None:
        """挂载业务前端 dist 为 MCP 根路径 "/" 的静态文件服务。

        仅 config_mode=='none' 时允许调用；与 mount_config_ui() 互斥。
        用于 open_mode=='fullpage' 场景（业务主界面由 keystone 前端通过反代承载）。
        底层挂 StaticFiles(directory, html=True) 到 Mount("/")，SPA 路由 fallback
        到 index.html（html=True 语义）。

        调用顺序：必须先 `set_config_mode("none")` 再调本方法。

        Raises:
            RuntimeError: 已调过 mount_config_ui（互斥）
            ValueError: config_mode 未设或非 'none'
        """
        if self._config_ui_dir is not None:
            raise RuntimeError(
                "mount_static_root 与 mount_config_ui 互斥，已调过后者"
            )
        if self._config_mode != "none":
            raise ValueError(
                f"mount_static_root 只能在 config_mode='none' 时调用，"
                f"当前为 {self._config_mode!r}"
            )
        self._static_root_dir = directory

    def _resolve_config_ui(self) -> dict | None:
        """解析最终 config_ui，归一到 ks-types 标准 {enabled,url}（A5）。
        manifest 来源优先，否则代码 mount_config_ui。兼容老 {path} 形态。
        """
        raw = load_manifest_config_ui(self.manifest_path)
        cu = self._normalize_config_ui(raw)
        if cu is None and self._config_ui_dir:
            cu = {"enabled": True, "url": f"{self._config_ui_path}/"}
        return cu

    @staticmethod
    def _normalize_config_ui(raw: dict | None) -> dict | None:
        """把任意来源的 config_ui 归一到 {enabled,url}（A5）。
        老 {path} 形态 → {enabled:True, url:path}；已有 enabled/url 则补全透传。
        """
        if not raw:
            return None
        if "url" in raw or "enabled" in raw:
            return {"enabled": bool(raw.get("enabled", True)), "url": raw.get("url", "")}
        path = raw.get("path")
        if path:
            return {"enabled": True, "url": path}
        return None

    def _derive_nav_state(self) -> tuple[str, str]:
        """从 self._nav 推 (nav_state, open_mode)，供 _validate_config_consistency 调对等矩阵。"""
        if self._nav is None:
            return (NAV_ABSENT, "")
        label = self._nav.get("label", "")
        category = self._nav.get("category", "")
        open_mode = self._nav.get("open_mode", "")
        if not label or not category or not open_mode:
            return (NAV_INVALID, "")
        if open_mode in ("dialog", "fullpage", "tab"):
            return (NAV_VALID, open_mode)
        return (NAV_INVALID, "")

    def _validate_config_consistency(self) -> None:
        """启动期 nav/config_mode/config_ui 组合一致性终检（A6），不一致即 raise（fail-fast）。"""
        nav_state, open_mode = self._derive_nav_state()
        cu = self._resolve_config_ui()
        has_config_ui = bool(cu and cu.get("enabled"))
        reason, ok = check_nav_config_consistency(
            nav_state, open_mode, self._config_mode or "", has_config_ui
        )
        if not ok:
            raise RuntimeError(f"ks_app: 配置组合不一致，应用入口无法工作: {reason}")

    def declare_nav(
        self,
        *,
        label: str,
        category: str,
        open_mode: str,
        icon: str | None = None,
        order: int | None = None,
        entry_path: str | None = None,
        required_perms: list[str] | None = None,
    ) -> None:
        """声明 MCP 在 keystone 后台左侧菜单的导航项（v0.4.0 新增，对齐 ks-types v0.5.0 MetaNavDecl）。

        Args:
            label: 菜单显示名（<= 12 字符，中文）
            category: 类目（应用 / 工具 / 配置 / 集成）
            open_mode: 打开方式（dialog / fullpage）
            icon: lucide-react 图标名（可选）
            order: 排序权重（默认 99）
            entry_path: 入口路径（默认 '/'）
            required_perms: 进入页面所需权限码（AND 语义；空数组 = admin 直通）
        """
        nav: dict = {"label": label, "category": category, "open_mode": open_mode}
        if icon is not None:
            nav["icon"] = icon
        if order is not None:
            nav["order"] = order
        if entry_path is not None:
            nav["entry_path"] = entry_path
        if required_perms is not None:
            nav["required_perms"] = required_perms
        self._nav = nav

    def declare_permission(
        self,
        *,
        code: str,
        label: str,
        default_roles: list[str] | None = None,
    ) -> None:
        """声明 MCP 的权限码目录条目（v0.4.0 新增，对齐 ks-types v0.5.0 MetaPermissionDecl）。

        Args:
            code: 权限码（mcp.{mcp_id}.{action} 格式）
            label: 中文显示名
            default_roles: 默认角色列表（MVP 只 ['admin']）
        """
        perm: dict = {"code": code, "label": label}
        if default_roles is not None:
            perm["default_roles"] = default_roles
        self._permissions.append(perm)

    def set_config_mode(self, mode: str) -> None:
        """设置配置模式（schema / iframe / none，v0.4.0 新增，对齐 ks-types v0.5.0）。

        与 mount_config_ui() 的关系（v0.4.0 共存约定）：
          - mode=='iframe'  → 必须配套 mount_config_ui(url=...) 提供接入信息
          - mode=='schema'  → 应不调 mount_config_ui（配置由 keystone SchemaForm 渲染）
          - mode=='none'    → 应不调 mount_config_ui
          - 不调本方法（mode==None）→ 走老语义（看 mount_config_ui 是否调过）
        """
        if mode not in ("schema", "iframe", "none"):
            raise ValueError(f"config_mode 必须是 schema/iframe/none，收到: {mode!r}")
        self._config_mode = mode

    def set_protocol_version(self, version: str) -> None:
        """设置 MCP 协议版本（SemVer 'MAJOR.MINOR'，MVP '1.0'，v0.4.0 新增）。"""
        self._protocol_version = version

    def set_config_status(self, status: str) -> None:
        """设置 MCP 配置状态（v0.4.0 新增，对齐 ks-types v0.5.0）。

        枚举：unconfigured / via_frontend / via_cli / mixed
        """
        if status not in ("unconfigured", "via_frontend", "via_cli", "mixed"):
            raise ValueError(f"config_status 枚举越界: {status!r}")
        self._config_status = status

    def on_startup(self, fn: Callable[[], Awaitable[None]]):
        """注册 startup 钩子（装饰器形式）。fn 必须是 async 函数。

        多个钩子按注册顺序执行。
        """
        if not inspect.iscoroutinefunction(fn):
            raise TypeError(
                f"on_startup 的 fn 必须是 async 函数，收到同步函数 {fn.__qualname__}"
            )
        self._startup_fns.append(fn)
        return fn

    def on_shutdown(self, fn: Callable[[], Awaitable[None]]):
        """注册 shutdown 钩子（装饰器形式）。fn 必须是 async 函数。

        多个钩子按注册的反序执行（后注册的先清理，符合资源释放语义）。
        """
        if not inspect.iscoroutinefunction(fn):
            raise TypeError(
                f"on_shutdown 的 fn 必须是 async 函数，收到同步函数 {fn.__qualname__}"
            )
        self._shutdown_fns.append(fn)
        return fn

    async def _call_tool(self, name: str, params: dict):
        """按 name 路由到注册的 handler 并调用。供 mcpproto 回调使用。"""
        tool_entry = self._tools.get(name)
        if not tool_entry:
            raise ValueError(f"tool {name!r} not found")
        return await tool_entry["handler"](**params)

    def _resolve_auth(self):
        return resolve_auth(
            code_mode=self._code_mode,
            manifest_path=self.manifest_path,
            env=dict(os.environ),
        )

    def _get_or_load_keystore(self):
        """懒加载 keystore（首次访问时 load）；并发首次调用用锁串行化。

        失败抛 RuntimeError：keystore.load 失败意味着 env 未配 + secret 文件缺 +
        fallback 目录不可写 —— 这是 SDK programmer-error 级的部署错配。
        对齐 Go getOrLoadKeystore（Go 侧 panic，Python 侧 RuntimeError 由 handler
        包 500）。

        /config-pubkey 与 /ks-config/{save,validate} 需要
        解密 → 需要密钥对；懒加载避免启动期空跑 I/O。
        """
        if self._keystore is not None:
            return self._keystore
        with self._keystore_lock:
            if self._keystore is None:
                from .keystore import load as load_keystore

                try:
                    self._keystore = load_keystore()
                except Exception as e:
                    raise RuntimeError(
                        f"keystore 加载失败 — 部署错配"
                        f"（env/secret/fallback 均不可用）: {e}"
                    ) from e
        return self._keystore

    def config_ui_middleware(self) -> Optional[Middleware]:
        """返回用于保护 /config-* 与代理端点的 Starlette Middleware。

        keystone_jwks 模式下返回真实 middleware（镜像 Go RequireConfigUIJWT）；
        其它模式返回 None（no-op，本地开发允许未鉴权直通）。

        Usage::

            mw = app.config_ui_middleware()
            if mw is not None:
                # 作为 middleware 传给 Starlette 或挂到 Route 前置

        Note: 每次调用会构造新的 JWKSVerifier 实例。若未来引入
        shared_verifier 字段，这里会改成复用同一实例避免重复缓存。
        """
        effective_mode, jwks_url = self._resolve_auth()
        if effective_mode != "keystone_jwks":
            return None
        from .auth.config_ui_middleware import require_config_ui_jwt_middleware
        return require_config_ui_jwt_middleware(JWKSVerifier(jwks_url))

    def create_app(self) -> Starlette:
        """构建并返回 Starlette 实例。"""
        # 启动期组合一致性终检（A6）：与 keystone 摄入诊断共用矩阵语义，不一致 fail-fast。
        self._validate_config_consistency()
        # 启动期 manifest 校验 + capability backend 元信息注入。
        # 注册的 capability canonical_name 必须存在于 manifest.provides.capabilities[]，
        # 否则抛 ManifestMismatch（启动期失败，不允许带病上线）。
        from .canonical import canonical as _canonical
        manifest_caps = load_manifest_capabilities(self.manifest_path)
        manifest_by_canonical: dict[str, ProvidesCapability] = {
            _canonical(self.app_id, c.name): c for c in manifest_caps
        }
        manifest_names = list(manifest_by_canonical.keys())
        for registered_name, entry in self._capabilities.items():
            if registered_name not in manifest_by_canonical:
                raise ManifestMismatch(registered=registered_name, manifest_names=manifest_names)
            spec = manifest_by_canonical[registered_name]
            entry.backend_kind = spec.backend.kind
            entry.backend_tool_name = spec.backend.tool_name
            entry.backend_path = spec.backend.path
            entry.backend_method = spec.backend.method
            entry.execution_mode = spec.execution_mode
            entry.timeout_ms = spec.timeout_ms
            entry.input_schema = spec.input_schema
        # manifest 声明但无 handler 的 capability 不在此报错/告警——交给下方 mcp_tool
        # 四象限裁决（可能是「复用已有 @app.tool」的合法复用项，或「无承载」报错）。
        # 与 Go wireMCPToolBackend 对齐（同步删除等价 warn 循环）。

        # mcp_tool 复用四象限。遍历 manifest 声明的 mcp_tool
        # capability（含无 handler 的复用项），按「是否有 handler × tool_name 是否命中
        # 已有 app.tool」决定 复用/生成/报错。existing_tools 是 wire 前快照。
        existing_tools = set(self._tools.keys())
        for cap in manifest_caps:
            if cap.backend.kind != "mcp_tool":
                continue
            cn = _canonical(self.app_id, cap.name)
            tool_name = cap.backend.tool_name
            if not tool_name:
                raise ValueError(
                    f"capability {cn!r} backend.kind=mcp_tool 但 "
                    f"manifest.provides.capabilities[].backend.tool_name 缺失"
                )
            entry = self._capabilities.get(cn)
            has_handler = entry is not None
            tool_exists = tool_name in existing_tools
            if has_handler and tool_exists:
                raise ValueError(
                    f"capability {cn!r} 的 backend.tool_name={tool_name!r} "
                    f"已被 @app.tool 注册占用（真冲突）；改名或避免冲突"
                )
            elif has_handler and not tool_exists:
                self._tools[tool_name] = {
                    "handler": _wrap_capability_as_tool(
                        entry, dispatcher_client_getter=self._get_dispatcher_client,
                    ),
                    "description": f"capability {cn}",
                    "input_schema": entry.input_schema,
                    "ui_binding": None,
                }
            elif not has_handler and tool_exists:
                # 复用已有 app.tool 作为 backend（join，keystone 透明）。
                # 复用降级：被复用的 tool handler 经 get_context() 取
                # ToolContext（caller_id/caller_kind/chain_id）。
                continue
            else:
                raise ValueError(
                    f"capability {cn!r} backend.tool_name={tool_name!r} 既无已注册 "
                    f"@app.tool 也无 @app.capability handler（无承载）"
                )

        effective_mode, jwks_url = self._resolve_auth()

        # http_endpoint backend 路径 wiring。
        # 注册 starlette POST route + 准备 ScopedJWTMiddleware path_map。
        http_capabilities = [
            (n, e) for n, e in self._capabilities.items()
            if e.backend_kind == "http_endpoint"
        ]
        http_path_to_name: dict[str, str] = {}
        if http_capabilities:
            from .scoped_jwt import ScopedJWTVerifier

            scoped_verifier = ScopedJWTVerifier(jwks_url=jwks_url or "")
            if self._scoped_jwt_test_keys:
                scoped_verifier._static_keys = dict(self._scoped_jwt_test_keys)
            self._scoped_verifier = scoped_verifier
            for cap_name, entry in http_capabilities:
                if not entry.backend_path:
                    raise ValueError(
                        f"capability {cap_name!r} backend.kind=http_endpoint 但 "
                        f"manifest.provides.capabilities[].backend.path 缺失"
                    )
                http_path_to_name[entry.backend_path] = cap_name
                method = (entry.backend_method or "POST").upper()
                self._custom_routes.append(
                    Route(
                        entry.backend_path,
                        _make_http_capability_endpoint(
                            entry, dispatcher_client_getter=self._get_dispatcher_client,
                        ),
                        methods=[method],
                    )
                )
        self._http_capability_path_map = http_path_to_name

        manifest_config_ui = self._resolve_config_ui()

        routes: list = health_routes(
            self.app_id,
            self.version,
            effective_mode,
            self._tools,
            self._health_checks,
            config_ui=manifest_config_ui,
            nav=self._nav,
            permissions=self._permissions,
            config_mode=self._config_mode,
            protocol_version=self._protocol_version,
            config_status=self._config_status,
        ) + [
            legacy_call_route(self._tools, self._call_tool),
            legacy_list_route(self._tools),
            mcp_route(self.app_id, self.version, self._tools, self._call_tool),
        ] + self._custom_routes

        # 若注册了 Config handle，自动挂四端点。
        # 对齐 Go app.go Mux() 里的 configHandles > 0 分支。
        if self._config_handles:
            from .config_handler import (
                make_config_pubkey_route,
                make_config_save_route,
                make_config_schema_route,
                make_config_validate_route,
            )

            routes.extend(
                [
                    Route(
                        "/config-schema",
                        make_config_schema_route(self),
                        methods=["GET"],
                    ),
                    Route(
                        "/config-pubkey",
                        make_config_pubkey_route(self),
                        methods=["GET"],
                    ),
                    Route(
                        "/ks-config/save",
                        make_config_save_route(self),
                        methods=["POST"],
                    ),
                    Route(
                        "/ks-config/validate",
                        make_config_validate_route(self),
                        methods=["POST"],
                    ),
                ]
            )

        if self._config_ui_dir:
            routes.append(
                Mount(
                    self._config_ui_path,
                    app=StaticFiles(directory=self._config_ui_dir, html=True),
                )
            )

        # 业务前端 dist 挂到根 "/"，必须在所有其他
        # routes 之后（Starlette 线性匹配，先具体后兜底）。
        # _SPAStaticFiles 提供 SPA fallback：未命中文件回 index.html（对齐 Go 行为）。
        if self._static_root_dir:
            routes.append(
                Mount(
                    "/",
                    app=_SPAStaticFiles(directory=self._static_root_dir, html=True),
                    name="app-root",
                )
            )

        middleware = []
        if effective_mode == "keystone_jwks":
            verifier = JWKSVerifier(jwks_url)
            middleware.append(
                Middleware(JWKSAuthMiddleware, verifier=verifier, protected_path="/mcp")
            )
            # 若注册了 Config handle 且 keystone_jwks
            # 模式，自动挂 ConfigUIJWTMiddleware 保护 /config-* + /ks-config/*。
            # 默认 protected_prefixes 保证不拦截 /meta、/healthz、/mcp 等非配置端点。
            if self._config_handles:
                from .auth.config_ui_middleware import ConfigUIJWTMiddleware

                middleware.append(
                    Middleware(
                        ConfigUIJWTMiddleware,
                        verifier=JWKSVerifier(jwks_url),
                    )
                )
        for middleware_cls, kwargs in self._middlewares:
            middleware.append(Middleware(middleware_cls, **kwargs))

        # http_endpoint backend 保护中间件，校验 scoped JWT + aud。
        if self._http_capability_path_map:
            from .auth.scoped_jwt_middleware import ScopedJWTMiddleware

            middleware.append(
                Middleware(
                    ScopedJWTMiddleware,
                    verifier=self._scoped_verifier,
                    path_to_canonical_name=dict(self._http_capability_path_map),
                )
            )

        @asynccontextmanager
        async def lifespan(_app):
            for fn in self._startup_fns:
                await fn()
            try:
                yield
            finally:
                for fn in reversed(self._shutdown_fns):
                    await fn()

        return Starlette(routes=routes, middleware=middleware, lifespan=lifespan)

    def _get_dispatcher_client(self):
        """Lazy 构造 DispatcherClient。需 KS_APP_TOKEN + KS_GATEWAY_URL；缺则返 None。"""
        if self._dispatcher_client is not None:
            return self._dispatcher_client
        token = os.environ.get("KS_APP_TOKEN", "")
        gateway = os.environ.get("KS_GATEWAY_URL", "")
        if not token or not gateway:
            return None
        from .keystone_client import DispatcherClient

        self._dispatcher_client = DispatcherClient(
            gateway_url=gateway, app_token=token,
        )
        return self._dispatcher_client

    def call_capability(self, canonical_name: str):
        """caller-side：构造 CapabilityCall 对象。

        用法::

            result = await app.call_capability("writer.list_articles").invoke(page=1)
            task   = await app.call_capability("image-gen.generate").submit(prompt="...")

        命名说明：协议层写的是 ``app.capability("...")``，但 ``app.capability``
        已被 @capability decorator factory 占用。SDK 实际暴露 ``call_capability``
        作为 caller 入口（Go SDK 同样：``App.CallCapability(name)``）。
        """
        from .task import CapabilityCall

        client = self._get_dispatcher_client()
        if client is None:
            raise RuntimeError(
                "ks-app: KS_APP_TOKEN + KS_GATEWAY_URL 未配置；caller-side "
                "capability 调用要求应用启动期注入这两个 env"
            )
        return CapabilityCall(
            canonical_name=canonical_name,
            dispatcher_client=client,
            events_client_getter=self._get_events_client,
        )

    def _get_events_client(self):
        """Lazy 构造 EventsClient。首次 submit 时被 CapabilityCall 间接调用。"""
        if self._events_client is not None:
            return self._events_client
        token = os.environ.get("KS_APP_TOKEN", "")
        gateway = os.environ.get("KS_GATEWAY_URL", "")
        if not token or not gateway:
            return None
        from .events import EventsClient

        self._events_client = EventsClient(
            gateway_url=gateway,
            app_token=token,
            event_mode=self._events_mode,
        )
        return self._events_client

    def run(self):
        """启动 HTTP 服务器（阻塞）。

        uvicorn 本身处理 SIGINT/SIGTERM 和优雅停机。
        若需自行管理生命周期，使用 create_app() 获取 Starlette 实例。
        """
        app = self.create_app()
        uvicorn.run(
            app,
            host=self._config["host"],
            port=self._config["port"],
            timeout_graceful_shutdown=10,
        )


def _wrap_capability_as_tool(entry: CapabilityEntry, *, dispatcher_client_getter):
    """把 @capability 注册的 handler 包成 MCP tool handler 形态。

    MCP tool handler 签名：``async def(**params) -> any``
    capability handler 签名：``async def(ctx: CapabilityContext, args: dict) -> dict``

    适配做的事：
    1. 从 ContextVar 取 _meta.ks_* 字段（mcpproto.{legacy,streamable} 已经在
       tools/call 路径里调 _set_meta 写好）
    2. 通过 ``dispatcher_client_getter`` lazy 取 DispatcherClient（progress 上报需要）
    3. 构造 CapabilityContext
    4. 以 (ctx, args) 调 capability handler
    """
    from .capability_context import build_context_from_meta
    from .context import (
        _ks_caller_id,
        _ks_caller_kind,
        _ks_chain_id,
        _ks_chain_snapshot,
        _ks_request_id,
        _ks_task_id,
        _ks_user_id,
    )

    async def adapter(**params):
        meta = {
            "ks_user_id": _ks_user_id.get(),
            "ks_caller_id": _ks_caller_id.get(),
            "ks_caller_kind": _ks_caller_kind.get(),
            "ks_chain_id": _ks_chain_id.get(),
            "ks_chain_snapshot": _ks_chain_snapshot.get(),
            "ks_task_id": _ks_task_id.get(),
            "ks_request_id": _ks_request_id.get(),
        }
        client = dispatcher_client_getter() if dispatcher_client_getter else None
        ctx = build_context_from_meta(
            meta=meta,
            canonical_name=entry.canonical_name,
            timeout_ms=entry.timeout_ms,
            dispatcher_client=client,
        )
        return await entry.handler(ctx, params)

    return adapter


def _make_http_capability_endpoint(entry: CapabilityEntry, *, dispatcher_client_getter):
    """构造 starlette endpoint：从 request.state.scoped_claims 解出 CapabilityContext 调 handler。"""
    from starlette.requests import Request
    from starlette.responses import JSONResponse

    from .capability_context import CapabilityContext

    async def endpoint(request: Request):
        claims = request.state.scoped_claims
        try:
            args = await request.json()
        except Exception:
            args = {}
        if not isinstance(args, dict):
            args = {}
        client = dispatcher_client_getter() if dispatcher_client_getter else None
        ctx = CapabilityContext(
            user_id=claims.user_id,
            caller_id=claims.caller_id,
            caller_kind=claims.caller_kind,
            chain_id=claims.chain_id,
            task_id="",
            request_id=claims.request_id,
            canonical_name=entry.canonical_name,
            timeout_ms=entry.timeout_ms,
            dispatcher_client=client,
        )
        result = await entry.handler(ctx, args)
        return JSONResponse(result if isinstance(result, dict) else {"result": result})

    return endpoint
