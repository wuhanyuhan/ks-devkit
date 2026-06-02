"""独立 DEK 落盘。

镜像 Go sdk/go/ksapp/keystore/dek.go。

  - `.local-dek`：32 字节随机对称密钥，与 X25519 私钥**完全无关**。
    X25519 私钥只用于端到端加密通道，DEK 只用于本地 mcp-config.enc 加解密。
    两者解耦使 X25519 私钥可随意轮换而不破坏历史密文。

  - `mcp-config.enc`：[version u8][nonce 12][AES-GCM ct+tag]，
    AAD = None（落盘只靠 DEK 保密，不绑定上下文）。

MVP 不做 DEK 轮换；容器重建丢 DEK 由 K8s PVC 承担。

错误信息不泄露 DEK 字节或 plaintext 字节。
"""
from __future__ import annotations

import os
from pathlib import Path

from cryptography.exceptions import InvalidTag

from ..crypto import AES_GCM_NONCE_LEN, decrypt_aes_gcm, encrypt_aes_gcm
from .loader import CORRUPTED_SUFFIX, KEY_DIR_MODE, KEY_FILE_MODE

# mcp-config.enc 文件首字节的版本号。MVP 固定为 1。
CONFIG_FILE_VERSION: int = 1

# DEK 固定字节长度（32 字节，AES-256 密钥长度）。
DEK_LEN: int = 32

# .local-dek 文件权限（0600，与 .mcp-key 一致）。
DEK_FILE_MODE = KEY_FILE_MODE

# mcp-config.enc 的最小合法长度：
#   - 1 字节 version
#   - 12 字节 AES-GCM nonce
#   - 16 字节 AES-GCM tag（无 plaintext 的极端情况）
CONFIG_FILE_MIN_SIZE: int = 1 + AES_GCM_NONCE_LEN + 16


def load_or_generate_dek(path: str) -> bytes:
    """加载或首次生成 32 字节独立 DEK。

    行为：
      - 文件存在：读出并校验长度 == 32，否则抛 ValueError（不自愈：静默重生会让
        mcp-config.enc 永久无法解密）。
      - 文件不存在：os.urandom(32) → MkdirAll parent 0700 → 写 0600。

    错误信息不泄露 DEK 字节。
    """
    p = Path(path)
    if p.exists():
        if p.is_dir():
            raise ValueError(f"keystore: DEK 路径 {path} 是目录")
        with open(path, "rb") as f:
            data = f.read()
        if len(data) != DEK_LEN:
            raise ValueError(
                f"keystore: DEK 文件长度 = {len(data)}, 期望 {DEK_LEN}（视为损坏）"
            )
        return data

    # 不存在 → 生成
    dek = os.urandom(DEK_LEN)
    parent = p.parent
    if str(parent) and str(parent) != ".":
        parent.mkdir(mode=KEY_DIR_MODE, parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, DEK_FILE_MODE)
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(dek)
    except Exception as e:
        raise OSError(f"keystore: 写 DEK 文件失败: {e}") from e
    return dek


def encrypt_config_to_file(cfg_path: str, dek: bytes, plaintext: bytes) -> None:
    """用 DEK 加密 plaintext 并按约定格式原子写入 cfg_path。

    文件格式：[version u8][nonce 12 bytes][AES-GCM ciphertext + 16-byte tag]

    - AAD = None（落盘只靠 DEK 保密）
    - 写入采用 .tmp + rename 原子模式
    - 失败时清理 .tmp，不留残文件
    - 父目录不存在自动 MkdirAll 0700
    """
    if len(dek) != DEK_LEN:
        raise ValueError(f"keystore: DEK 长度 = {len(dek)}, 期望 {DEK_LEN}")
    ct, nonce = encrypt_aes_gcm(dek, plaintext, None)
    # 拼装 [version][nonce][ct+tag]
    buf = bytes([CONFIG_FILE_VERSION]) + nonce + ct

    parent = Path(cfg_path).parent
    if str(parent) and str(parent) != ".":
        parent.mkdir(mode=KEY_DIR_MODE, parents=True, exist_ok=True)
    tmp = cfg_path + ".tmp"
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, DEK_FILE_MODE)
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(buf)
    except Exception as e:
        try:
            os.remove(tmp)
        except OSError:
            pass
        raise OSError(f"keystore: 写 mcp-config.enc.tmp 失败: {e}") from e

    try:
        os.replace(tmp, cfg_path)
    except Exception as e:
        try:
            os.remove(tmp)
        except OSError:
            pass
        raise OSError(
            f"keystore: rename mcp-config.enc.tmp → final 失败: {e}"
        ) from e


def decrypt_config_from_file(cfg_path: str, dek: bytes) -> bytes:
    """读取并解密 mcp-config.enc，返回 plaintext。

    损坏分支处理（区分 .corrupted 备份与否）：
      - 长度 < CONFIG_FILE_MIN_SIZE → 备份 .corrupted + raise（完全无效，自动隔离）
      - data[0] != CONFIG_FILE_VERSION → 仅 raise（版本语义清晰，运维手工处理）
      - AES-GCM 解密失败（含错 DEK / 篡改）→ 备份 .corrupted + raise

    文件不存在 → 直接抛 FileNotFoundError（不备份）。

    错误信息不泄露 DEK 字节或 plaintext 字节。
    """
    if len(dek) != DEK_LEN:
        raise ValueError(f"keystore: DEK 长度 = {len(dek)}, 期望 {DEK_LEN}")
    with open(cfg_path, "rb") as f:
        data = f.read()
    if len(data) < CONFIG_FILE_MIN_SIZE:
        _backup_corrupted(cfg_path)
        raise ValueError(
            f"keystore: mcp-config.enc 长度 = {len(data)}, "
            f"期望 >= {CONFIG_FILE_MIN_SIZE}（视为损坏）"
        )
    if data[0] != CONFIG_FILE_VERSION:
        # version 不匹配：不备份，让运维处理
        raise ValueError(
            f"keystore: mcp-config.enc version = {data[0]}, 期望 {CONFIG_FILE_VERSION}"
        )
    nonce = data[1 : 1 + AES_GCM_NONCE_LEN]
    ct = data[1 + AES_GCM_NONCE_LEN :]
    try:
        plaintext = decrypt_aes_gcm(dek, nonce, ct, None)
    except InvalidTag as e:
        _backup_corrupted(cfg_path)
        # 注意：不把 dek / plaintext 字节放进 message
        raise ValueError("keystore: mcp-config.enc 解密失败（AES-GCM tag）") from e
    return plaintext


def _backup_corrupted(cfg_path: str) -> None:
    """把损坏的文件备份到 path + ".corrupted"。

    备份失败也不阻塞主流程：调用方只关心解密失败 + 文件已隔离的语义；
    额外错误吞掉以保持错误返回值的"为什么解密失败"语义清晰。
    """
    try:
        os.replace(cfg_path, cfg_path + CORRUPTED_SUFFIX)
    except OSError:
        pass
