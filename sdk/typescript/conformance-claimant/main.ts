/**
 * ks-app-ts SDK 的 conformance claimant。
 *
 * 声称遵守 ks-devkit/conformance/auth/ v1.0.0 契约。
 * 除 echo 工具外不做任何业务，行为被 conformance 测试冻结。
 *
 * 不要修改 echo 的名字、schema 或返回值——否则 conformance case 16/17 会失败。
 */

import { createApp } from "@wuhanyuhan/ks-app";
import { z } from "zod";

const app = createApp({
  id: "conformance-claimant",
  version: "conformance-v1.0.0",
  auth: "keystone_jwks",
});

app.tool(
  "echo",
  {
    description: "Echo message as-is (conformance test tool)",
    inputSchema: { message: z.string() },
  },
  async ({ message }) => ({ echoed: message })
);

await app.run();
