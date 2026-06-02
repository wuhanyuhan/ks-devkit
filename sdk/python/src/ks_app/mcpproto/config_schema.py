"""config schema v0.6.0 镜像 — 对应 ks-types config_schema.go。"""
from pydantic import BaseModel, Field
from typing import Any


class ConfigSchemaResponse(BaseModel):
    schema_: dict[str, Any] = Field(alias="schema")
    ui_schema: dict[str, Any]
    version: str

    class Config:
        populate_by_name = True


class ConfigPubkeyTrust(BaseModel):
    status: str
    reason: str | None = None
    app_id: str | None = None
    app_version: str | None = None
    source: str | None = None
    signer_kid: str | None = None
    package_sha256: str | None = None
    manifest_sha256: str | None = None
    verified_at: str | None = None
    requires_user_confirmation: bool = True


class ConfigPubkeyResponse(BaseModel):
    pubkey: str
    fingerprint: str
    algorithm: str
    created_at: str
    trust: ConfigPubkeyTrust | None = None


class AppPackageSignature(BaseModel):
    signature_b64: str
    kid: str
    public_key_url: str
    domain: str
    signed_payload_b64: str
    package_sha256: str
    manifest_sha256: str
    signed_at: str
    verification_state: str | None = None


class EncryptedConfigPayload(BaseModel):
    algorithm: str
    ephemeral_pubkey: str
    nonce: str
    aad_fields: dict[str, Any]
    aad_canonical: str
    ciphertext: str
    idempotency_key: str


class ConfigApplyResult(BaseModel):
    applied_at: str
    version: int
