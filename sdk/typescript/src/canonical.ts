/**
 * canonical 派生：与 Go kstypes.Canonical / Python canonical 等价。
 * 三语言各自实现，靠 sdk/shared-fixtures/canonical_derivation.json wire-compat 锁一致。
 *
 * 去前缀：provides 作者写裸名 name；canonical_name 由 SDK 派生
 * <app_id>.<name>。caller 侧 requires / callCapability 写全名、不经此派生（不对称）。
 */
export function canonical(appId: string, name: string): string {
  return `${appId}.${name}`;
}
