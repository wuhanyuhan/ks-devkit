"""ks-readiness 端点：应用上报 init_task 门运行时状态 + 接受 keystone 触发初始化。

与 health/meta 同通路、无鉴权（平台内网 server-to-server）。wire 形状与 ks-types
ReadinessReport/ReadinessGateState/ReadinessInitRequest 对齐（Python 无 ks-types 包，
用 dict 镜像；由 sdk/shared-fixtures conformance 锁定跨语言一致）。
"""
import asyncio
import inspect
from typing import Awaitable, Callable, Optional

from starlette.requests import Request
from starlette.responses import JSONResponse, Response
from starlette.routing import Route

# InitProgress：handler 执行期调用上报进度（percent 0-100、message 人话）。
InitProgress = Callable[[int, str], None]
# InitTaskHandler：async 函数，经 progress 上报；正常返回=ready，抛异常=failed。
InitTaskHandler = Callable[[InitProgress], Awaitable[None]]


class InitTaskRuntime:
    """单个 init_task 门的 handler + 运行时状态（内存态，重启重置 pending）。"""

    def __init__(self, handler: InitTaskHandler):
        self.handler = handler
        self.status = "pending"  # pending / running / ready / failed
        self.progress: Optional[int] = None
        self.message = ""

    def snapshot(self, gate_id: str) -> dict:
        state = {"id": gate_id, "status": self.status}
        if self.progress is not None:
            state["progress"] = self.progress
        if self.message:
            state["message"] = self.message
        return state


def make_init_task_decorator(registry: dict):
    """工厂：返回绑定到 registry 的 @init_task(gate_id) decorator。"""

    def decorator(gate_id: str):
        def inner(handler: InitTaskHandler) -> InitTaskHandler:
            if not inspect.iscoroutinefunction(handler):
                raise TypeError(f"init_task {gate_id!r} 的 handler 必须是 async 函数")
            if gate_id in registry:
                raise ValueError(f"init_task {gate_id!r} 已经注册过了，禁止重复注册")
            registry[gate_id] = InitTaskRuntime(handler)
            return handler

        return inner

    return decorator


async def _run_init_task(rt: InitTaskRuntime):
    def progress(percent: int, message: str):
        rt.progress = percent
        rt.message = message

    try:
        await rt.handler(progress)
        rt.status = "ready"
        rt.progress = 100
    except Exception as e:  # noqa: BLE001 - 失败统一收敛为 failed 态
        rt.status = "failed"
        rt.message = str(e)


def readiness_routes(init_tasks: dict) -> list:
    """返回 GET /ks-readiness + POST /ks-readiness/init 两路由（无鉴权，同 health/meta 通路）。"""

    async def readiness(request: Request) -> Response:
        gates = [rt.snapshot(gid) for gid, rt in sorted(init_tasks.items())]
        return JSONResponse({"gates": gates})

    async def readiness_init(request: Request) -> Response:
        try:
            body = await request.json()
        except Exception as e:
            return JSONResponse(
                {"code": "ERR_SCHEMA", "message": f"request body JSON 解析失败: {e}", "data": None},
                status_code=400,
            )
        if not isinstance(body, dict):
            return JSONResponse(
                {"code": "ERR_SCHEMA", "message": "request body 必须是 JSON object", "data": None},
                status_code=400,
            )
        gate_id = body.get("gate_id", "")
        rt = init_tasks.get(gate_id)
        if rt is None:
            return JSONResponse(
                {"code": "ERR_GATE_NOT_FOUND", "message": f"未注册的 init_task 门: {gate_id}", "data": None},
                status_code=404,
            )
        if rt.status == "running":
            return JSONResponse({"code": 0, "message": "初始化进行中", "data": rt.snapshot(gate_id)})
        rt.status = "running"
        rt.progress = None
        rt.message = ""
        asyncio.create_task(_run_init_task(rt))
        return JSONResponse({"code": 0, "message": "初始化已触发", "data": rt.snapshot(gate_id)})

    return [
        Route("/ks-readiness", readiness, methods=["GET"]),
        Route("/ks-readiness/init", readiness_init, methods=["POST"]),
    ]
