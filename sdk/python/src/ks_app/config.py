import os


def load_config() -> dict:
    """从环境变量加载配置。暂不读取 YAML 文件。"""
    return {
        "port": int(os.environ.get("KS_APP_PORT", "8080")),
        "host": os.environ.get("KS_APP_HOST", "0.0.0.0"),
    }
