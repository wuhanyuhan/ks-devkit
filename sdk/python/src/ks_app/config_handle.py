"""Config handle + new_config 工厂。

镜像 Go ksapp/config_handle.go。核心 API：

    cfg = new_config(app, MyConfig, ConfigSpec(
        on_validate=my_validate_fn,
        on_apply=my_apply_fn,
    ))
    # cfg 自动挂 /config-schema / /config-pubkey
    #   / /ks-config/save / /ks-config/validate 四端点

与 Go 的偏差：
  - Go 用泛型 `Config[T]` + `ConfigSpec[T]`；Python 用 `Generic[T]` + pydantic BaseModel
    约束 T；用户侧 API 等价。
  - Go `ptr atomic.Pointer[T]` 保证 Get() 无锁；Python 靠 GIL + 简单赋值实现
    等价原子读（对齐 asyncio 场景下的读一致性）。
  - `on_validate` / `on_apply` 必须是 async 函数（对齐 Go `ctx context.Context` 签名）。
  - `ensure_idemp_lru` / `handle_save` 里调用 keystore 的部分用 late import，
    依赖 keystore（密钥）+ idempotency（幂等）模块。
"""
from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass
from typing import Any, Awaitable, Callable, Generic, TypeVar

from pydantic import BaseModel, ValidationError

from .ksconfig import reflect_config_schema

# wire-level 错误码常量。
# 内部使用：不导出到 ks_app.__init__，避免污染 public API；跨模块需要时再提升。
ERR_SCHEMA = "ERR_SCHEMA"
ERR_VALIDATE = "ERR_VALIDATE"
ERR_STORE = "ERR_STORE"
ERR_APPLY = "ERR_APPLY"
ERR_INTERNAL = "ERR_INTERNAL"

T = TypeVar("T", bound=BaseModel)


@dataclass
class ConfigSpec(Generic[T]):
    """new_config 入参：承载 on_validate / on_apply async 回调。

    镜像 Go `ConfigSpec[T any]`。两个字段均可为 None，缺省时对应回调被跳过。
    """

    on_validate: Callable[[T], Awaitable[None]] | None = None
    on_apply: Callable[[T], Awaitable[None]] | None = None


class Config(Generic[T]):
    """类型化配置 handle；内部持有当前配置快照 + schema/ui_schema + 持久化字段。

    典型生命周期：
      1. `new_config(app, MyConfig, ConfigSpec(...))` 注册到 app
      2. `cfg.bootstrap_persistence(persist_path, dek_path, dek)` 由 Bootstrap 注入
      3. `cfg.handle_save(new_cfg)` /  `cfg.handle_validate(new_cfg)` 由
         端点 handler 调用
      4. 业务代码用 `cfg.get()` 拿当前快照（None 表示尚未 save）

    所有 handle_* / *_from_bytes 方法都是 async，对齐 Go `ctx context.Context` 签名
    且允许 on_validate / on_apply 里做 await 操作。
    """

    def __init__(
        self,
        model_cls: type[T],
        spec: ConfigSpec[T],
    ):
        self._model_cls = model_cls
        self._spec = spec
        self._name = model_cls.__qualname__
        schema, ui_schema = reflect_config_schema(model_cls)
        self._schema = schema
        self._ui_schema = ui_schema
        # 当前配置快照；None 表示未 save。Python 简单赋值在 GIL 下原子，
        # 对应 Go `atomic.Pointer[T]`。
        self._current: T | None = None
        # 持久化字段：由 bootstrap_persistence 注入。
        self._persist_path: str = ""
        self._dek_path: str = ""
        self._dek: bytes | None = None
        # 幂等 LRU；懒初始化字段。
        self._idemp_lru: Any = None
        # handle_save 串行化锁（对齐 Go config_handle.go writeMu）。
        # 让 on_validate → _persist_encrypted → 内存切换 → on_apply → 错误回滚
        # 整段串行化，禁止两次并发 save 交错。
        self._lock = asyncio.Lock()

    # ---- public API ----

    def get(self) -> T | None:
        """返回当前配置快照；未 save 时返回 None。"""
        return self._current

    # ---- 包私有：供 app / handler 遍历 ----

    def type_name(self) -> str:
        """返回注册时记录的类型名（model_cls.__qualname__），用于日志 / 路由分发。"""
        return self._name

    def schema_json(self) -> tuple[dict[str, Any], dict[str, Any]]:
        """返回 (JSON Schema, UI Schema)，供 /config-schema 端点序列化返回。"""
        return self._schema, self._ui_schema

    def bootstrap_persistence(
        self,
        persist_path: str,
        dek_path: str,
        dek: bytes,
    ) -> None:
        """由 App Bootstrap 注入持久化所需路径 + DEK。

        对齐 Go `bootstrapPersistence`：把"NewConfig 注册但 Bootstrap 漏调"的暴露
        从"首次 save panic"提前到"启动完成前 panic"（由 App.Bootstrap 校验
        `has_dek()` fail-fast）。
        """
        self._persist_path = persist_path
        self._dek_path = dek_path
        self._dek = dek

    def has_dek(self) -> bool:
        """返回 DEK 是否已注入；App Bootstrap 末尾逐个校验，false 则 fail-fast。"""
        return self._dek is not None

    def ensure_idemp_lru(self) -> Any:
        """返回 per-handle 幂等 LRU（懒初始化）。

        真实 LRU 实现在 `idempotency.py`，由端点 handler 触发。
        """
        if self._idemp_lru is None:
            # late import idempotency 模块
            from .idempotency import IdempotencyLRU  # type: ignore[import-not-found]
            self._idemp_lru = IdempotencyLRU(capacity=64, ttl_seconds=600)
        return self._idemp_lru

    # ---- validate / save 流程 ----

    async def handle_validate(self, new_cfg: T) -> None:
        """被 /ks-config/validate 端点调用。MVP 只走 on_validate 回调。

        on_validate 抛异常 → 原样传播（调用方 validate_from_bytes 会包成 ERR_VALIDATE）。
        """
        if self._spec.on_validate is None:
            return
        await self._spec.on_validate(new_cfg)

    async def handle_save(self, new_cfg: T) -> None:
        """被 /ks-config/save 端点调用。完整流程（镜像 Go handleSave）：

          1. on_validate 校验 new_cfg —— 失败 → 抛 _BizException(ERR_VALIDATE)
          2. `_persist_encrypted` 通过 DEK 加密落盘 —— 失败 → _BizException(ERR_STORE)
          3. 切换内存快照（self._current = new_cfg）
          4. on_apply 业务侧应用（如重建 LLM client）
             失败 → 内存 + 磁盘双回滚到 old_cfg，再抛 _BizException(ERR_APPLY)

        三个阶段用 `_BizException` 标注错误码，让 apply_save_from_bytes 能精确映射
        HTTP status —— 避免 on_validate 失败在 save 路径归 ERR_INTERNAL。

        调用前提：self._dek 必须已注入（由 Bootstrap 或测试代码赋值）；
        None 时直接 raise RuntimeError（fail-fast，对齐 Go 实现）。
        """
        if self._dek is None:
            raise RuntimeError(
                "ksapp: handle_save 调用前未注入 dek（Bootstrap 缺陷）— "
                "必须先 load_or_generate_dek 后调 bootstrap_persistence"
            )

        # 串行化整段 save 流程（对齐 Go writeMu.Lock() / defer Unlock()）。
        async with self._lock:
            # 阶段 1: on_validate
            if self._spec.on_validate is not None:
                try:
                    await self._spec.on_validate(new_cfg)
                except _BizException:
                    raise
                except Exception as e:
                    raise _BizException(ERR_VALIDATE, str(e)) from e

            # 阶段 2: persist
            try:
                self._persist_encrypted(new_cfg)
            except _BizException:
                raise
            except Exception as e:
                raise _BizException(ERR_STORE, str(e)) from e

            # 阶段 3: snapshot 切换（纯内存操作，不会抛业务错）
            old_cfg = self._current
            self._current = new_cfg

            # 阶段 4: on_apply
            if self._spec.on_apply is not None:
                try:
                    await self._spec.on_apply(new_cfg)
                except Exception as e:
                    # 双回滚：内存 + 磁盘
                    self._current = old_cfg
                    self._rollback_persisted(old_cfg)
                    if isinstance(e, _BizException):
                        raise
                    raise _BizException(ERR_APPLY, str(e)) from e

    async def validate_from_bytes(
        self,
        plaintext: bytes,
    ) -> tuple[str, str]:
        """从解密后的 plaintext JSON 字节恢复 new_cfg 并仅走 handle_validate。

        不落盘、不切换内存、不触发 on_apply。

        Returns:
            (err_code, err_msg) — `("", "")` 表示成功。失败分支：
            - JSON 解析失败 → `(ERR_SCHEMA, msg)`
            - pydantic 校验失败 → `(ERR_SCHEMA, msg)`
            - on_validate 抛异常 → `(ERR_VALIDATE, msg)`
        """
        try:
            new_cfg = self._parse_from_bytes(plaintext)
        except _ParseError as e:
            return ERR_SCHEMA, str(e)

        try:
            await self.handle_validate(new_cfg)
        except Exception as e:
            return ERR_VALIDATE, str(e)
        return "", ""

    async def apply_save_from_bytes(
        self,
        plaintext: bytes,
        aad_fields: dict[str, Any],
    ) -> tuple[int, int, str, str]:
        """从解密后的 plaintext JSON 字节恢复 new_cfg 并走 handle_save 完整流程
        （对齐 Go applySaveFromBytes）。

        Returns:
            (applied_ver, http_status, err_code, err_msg)
            - 成功：`(applied_ver, 0, "", "")`
            - 失败：`(0, http_status, err_code, err_msg)`

        错误分支（handle_save 内部已按阶段用 `_BizException` 打码，这里按 code 映射 HTTP）：
          - ERR_SCHEMA (422): plaintext JSON 反序列化 / pydantic 校验失败
          - ERR_VALIDATE (422): on_validate 失败
          - ERR_STORE (500): _persist_encrypted 失败（含 dek 未注入的 RuntimeError）
          - ERR_APPLY (500): on_apply 失败
          - ERR_INTERNAL (500): 其它未分类异常（兜底）

        applied_ver 从 `aad_fields["config_version"]` 读取（JSON 数字默认 float64 → int）。
        """
        try:
            new_cfg = self._parse_from_bytes(plaintext)
        except _ParseError as e:
            return 0, 422, ERR_SCHEMA, str(e)

        try:
            await self.handle_save(new_cfg)
        except _BizException as be:
            return 0, _http_status_for_code(be.code), be.code, be.message
        except RuntimeError as e:
            # dek 未注入 fail-fast —— 文件 I/O / 持久化相关，归 ERR_STORE。
            return 0, 500, ERR_STORE, str(e)
        except Exception as e:
            return 0, 500, ERR_INTERNAL, str(e)

        raw_ver = aad_fields.get("config_version", 0)
        try:
            applied_ver = int(raw_ver)
        except (TypeError, ValueError):
            applied_ver = 0
        return applied_ver, 0, "", ""

    def load_persisted(self) -> T | None:
        """从 mcp-config.enc 加载并反序列化为 model_cls 实例；任一步失败返回 None。

        调用前提：self._dek 必须已注入；None 时 raise RuntimeError（fail-fast）。
        keystore 模块经 late import 加载；生产路径上只由 Bootstrap 调用。
        """
        if self._dek is None:
            raise RuntimeError(
                "ksapp: load_persisted 调用前未注入 dek（Bootstrap 缺陷）"
            )
        if not self._persist_path:
            return None
        # late import keystore 模块
        from .keystore import decrypt_config_from_file  # type: ignore[import-not-found]
        try:
            data = decrypt_config_from_file(self._persist_path, self._dek)
        except Exception:
            return None
        try:
            return self._model_cls.model_validate_json(data)
        except ValidationError:
            return None

    # ---- 内部 helper ----

    def _parse_from_bytes(self, plaintext: bytes) -> T:
        """反序列化 + pydantic 校验；失败统一抛 _ParseError（外层映射 ERR_SCHEMA）。"""
        try:
            raw = json.loads(plaintext)
        except (ValueError, TypeError) as e:
            raise _ParseError(f"JSON 反序列化失败: {e}") from e
        try:
            return self._model_cls.model_validate(raw)
        except ValidationError as e:
            raise _ParseError(f"Schema 校验失败: {e}") from e

    def _persist_encrypted(self, new_cfg: T) -> None:
        """序列化 + DEK 加密落盘（handle_save 步骤 2）。

        keystore 模块经 late import 加载，本方法 late import 触发
        前提是 handle_save 已通过 dek is None 校验。
        """
        # late import keystore 模块
        from .keystore import encrypt_config_to_file  # type: ignore[import-not-found]
        data = new_cfg.model_dump_json().encode("utf-8")
        encrypt_config_to_file(self._persist_path, self._dek, data)

    def _rollback_persisted(self, old_cfg: T | None) -> None:
        """on_apply 失败后把磁盘回滚到 old_cfg。

        - old_cfg is None → 删 persist_path（首次 save 失败）
        - old_cfg 非 None → 用旧值重写（已成功过的重写）

        任何错误吞掉（对齐 Go rollbackPersisted 注释约定：磁盘可能留新值 / 内存留旧值，
        下次启动 load_persisted 会拿回 new_cfg，用户重新触发 save 修复）。
        """
        # late import keystore 模块
        try:
            from .keystore import encrypt_config_to_file  # type: ignore[import-not-found]
        except ImportError:
            # keystore 不存在时回滚不可用（测试路径不会走到这里）
            return

        if old_cfg is None:
            import os
            try:
                if self._persist_path:
                    os.remove(self._persist_path)
            except OSError:
                # TODO: 引入结构化 logger 后此处 Warn 记录删除失败
                pass
            return

        try:
            data = old_cfg.model_dump_json().encode("utf-8")
            encrypt_config_to_file(self._persist_path, self._dek, data)
        except Exception:
            # TODO: 引入结构化 logger 后此处 Warn 记录回滚写盘失败
            pass


# ---- 工厂 + 错误类型 ----


def new_config(
    app: Any,
    model_cls: type[T],
    spec: ConfigSpec[T] | None = None,
) -> Config[T]:
    """注册配置 handle 到 App。

    同一 app 同一 model_cls 只能调用一次；重复调抛 ValueError。
    app 为 None 抛 TypeError。model_cls 不是 BaseModel 子类抛 TypeError。

    Args:
        app: `ks_app.App` 实例（传 None 会抛 TypeError）
        model_cls: pydantic BaseModel 子类
        spec: ConfigSpec；None 时用默认空 spec

    Returns:
        类型化 Config handle；自动挂 4 端点。
    """
    if app is None:
        raise TypeError("new_config: app 不能为 None")
    if not (isinstance(model_cls, type) and issubclass(model_cls, BaseModel)):
        raise TypeError(
            f"new_config: model_cls 必须是 pydantic BaseModel 子类，收到 {model_cls!r}"
        )
    if spec is None:
        spec = ConfigSpec()

    tname = model_cls.__qualname__
    # 重复注册校验（对齐 Go registerConfigHandleSlot panic on duplicate）。
    # 需要 App 已在 __init__ 中初始化 _config_handle_types / _config_handles。
    types_set: set[str] = app._config_handle_types
    if tname in types_set:
        raise ValueError(
            f"new_config: 类型 {tname!r} 已注册过，同一 app 同一 model 只能调一次"
        )
    types_set.add(tname)

    handle: Config[T] = Config(model_cls, spec)
    app._config_handles.append(handle)
    return handle


class _ParseError(Exception):
    """JSON 反序列化 / pydantic 校验失败；外层映射为 ERR_SCHEMA。"""


class _BizException(Exception):
    """业务错误包装（code + message）。

    `handle_save` 内部按阶段抛对应错误码（ERR_VALIDATE / ERR_STORE / ERR_APPLY），
    由 `apply_save_from_bytes` 映射到 HTTP status。对齐 Go `errBizf` + handler switch。
    """

    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code
        self.message = message


def _http_status_for_code(code: str) -> int:
    """把错误码映射到 HTTP status（对齐 Go applySaveFromBytes switch）。"""
    if code in (ERR_VALIDATE, ERR_SCHEMA):
        return 422
    if code in (ERR_STORE, ERR_APPLY):
        return 500
    return 500
