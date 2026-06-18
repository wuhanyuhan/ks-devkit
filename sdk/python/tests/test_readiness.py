import asyncio
import threading
import time

from starlette.testclient import TestClient

from ks_app import App


def _app(monkeypatch):
    monkeypatch.delenv("KEYSTONE_JWKS_URL", raising=False)
    monkeypatch.delenv("KS_APP_AUTH_MODE", raising=False)
    return App("test-app", manifest_path="nonexistent.yaml")


def test_readiness_reports_registered_init_tasks(monkeypatch):
    app = _app(monkeypatch)

    @app.init_task("corpus_embed")
    async def corpus_embed(progress):
        progress(50, "halfway")

    client = TestClient(app.create_app())
    r = client.get("/ks-readiness")
    assert r.status_code == 200
    assert r.json() == {"gates": [{"id": "corpus_embed", "status": "pending"}]}


def test_readiness_init_runs_to_ready(monkeypatch):
    app = _app(monkeypatch)

    @app.init_task("warm")
    async def warm(progress):
        progress(100, "done")

    client = TestClient(app.create_app())
    r = client.post("/ks-readiness/init", json={"gate_id": "warm"})
    assert r.status_code == 200
    # 触发后轮询 GET 直到 ready（后台 task 在同一事件循环上收敛）。
    status = None
    for _ in range(50):
        status = client.get("/ks-readiness").json()["gates"][0]["status"]
        if status == "ready":
            break
        time.sleep(0.02)
    assert status == "ready"


def test_readiness_slow_init_keeps_running_and_noops_duplicate_trigger(monkeypatch):
    app = _app(monkeypatch)
    started = threading.Event()
    release = threading.Event()
    calls = 0

    @app.init_task("slow")
    async def slow(progress):
        nonlocal calls
        calls += 1
        progress(10, "working")
        started.set()
        await asyncio.to_thread(release.wait)
        progress(100, "done")

    with TestClient(app.create_app()) as client:
        r = client.post("/ks-readiness/init", json={"gate_id": "slow"})
        assert r.status_code == 200
        assert started.wait(timeout=1)

        rep = client.get("/ks-readiness").json()
        assert rep == {
            "gates": [
                {
                    "id": "slow",
                    "status": "running",
                    "progress": 10,
                    "message": "working",
                }
            ]
        }

        r2 = client.post("/ks-readiness/init", json={"gate_id": "slow"})
        assert r2.status_code == 200
        assert r2.json()["message"] == "初始化进行中"
        assert calls == 1

        release.set()
        status = None
        for _ in range(50):
            status = client.get("/ks-readiness").json()["gates"][0]["status"]
            if status == "ready":
                break
            time.sleep(0.02)
        assert status == "ready"
        assert calls == 1


def test_readiness_init_unknown_gate_404(monkeypatch):
    app = _app(monkeypatch)

    @app.init_task("warm")
    async def warm(progress):
        pass

    client = TestClient(app.create_app())
    r = client.post("/ks-readiness/init", json={"gate_id": "nope"})
    assert r.status_code == 404


def test_readiness_no_init_tasks_no_endpoint(monkeypatch):
    app = _app(monkeypatch)
    client = TestClient(app.create_app())
    r = client.get("/ks-readiness")
    assert r.status_code == 404
