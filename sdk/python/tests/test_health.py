from starlette.routing import Route

from ks_app.health import health_routes


def test_health_routes_returns_three():
    routes = health_routes("my-app", "0.1.0", "none", {})
    assert len(routes) == 3
    paths = {r.path for r in routes}
    assert paths == {"/healthz", "/readyz", "/meta"}


def test_health_routes_are_starlette_routes():
    routes = health_routes("my-app", "0.1.0", "none", {})
    for r in routes:
        assert isinstance(r, Route)
