"""ks_app.keystore — X25519 私钥三来源加载 + 独立 DEK 落盘。

暴露：
  - Source / Keypair / Keystore / LoadOptions / load：私钥三来源加载
  - RotateOptions / RotateResult / rotate / prune_old：X25519 密钥轮换
  - CONFIG_FILE_VERSION / DEK_LEN / load_or_generate_dek /
    encrypt_config_to_file / decrypt_config_from_file：独立 DEK 落盘
"""
from .dek import (
    CONFIG_FILE_MIN_SIZE,
    CONFIG_FILE_VERSION,
    DEK_LEN,
    decrypt_config_from_file,
    encrypt_config_to_file,
    load_or_generate_dek,
)
from .loader import (
    CORRUPTED_SUFFIX,
    DEFAULT_FALLBACK_FILE,
    DEFAULT_FALLBACK_OLD,
    DEFAULT_SECRET_FILE,
    DEFAULT_SECRET_FILE_OLD,
    ENV_PRIVKEY_B64,
    ENV_PRIVKEY_FILE,
    ENV_PRIVKEY_OLD_B64,
    ENV_PRIVKEY_OLD_FILE,
    Keypair,
    Keystore,
    LoadOptions,
    PERSISTED_KEY_VERSION,
    Source,
    load,
)
from .rotate import (
    OLD_KEY_RETENTION,
    OLD_KEY_RETENTION_DAYS,
    RotateOptions,
    RotateResult,
    prune_old,
    rotate,
)

__all__ = [
    # loader
    "Source",
    "Keypair",
    "Keystore",
    "LoadOptions",
    "load",
    "ENV_PRIVKEY_B64",
    "ENV_PRIVKEY_OLD_B64",
    "ENV_PRIVKEY_FILE",
    "ENV_PRIVKEY_OLD_FILE",
    "DEFAULT_SECRET_FILE",
    "DEFAULT_SECRET_FILE_OLD",
    "DEFAULT_FALLBACK_FILE",
    "DEFAULT_FALLBACK_OLD",
    "PERSISTED_KEY_VERSION",
    "CORRUPTED_SUFFIX",
    # rotate
    "RotateOptions",
    "RotateResult",
    "rotate",
    "prune_old",
    "OLD_KEY_RETENTION",
    "OLD_KEY_RETENTION_DAYS",
    # dek
    "CONFIG_FILE_VERSION",
    "CONFIG_FILE_MIN_SIZE",
    "DEK_LEN",
    "load_or_generate_dek",
    "encrypt_config_to_file",
    "decrypt_config_from_file",
]
