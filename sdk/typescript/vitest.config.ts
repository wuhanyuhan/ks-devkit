import { defineConfig } from 'vitest/config'

/**
 * @wuhanyuhan/ks-app vitest 配置。
 *
 * 显式 exclude 子 workspace（squad-widget-sdk / conformance-claimant），让它们各自独立跑测试，
 * 避免 root vitest 把子包的测试也吃进来（子包可能依赖 happy-dom 等不同环境）。
 */
export default defineConfig({
  test: {
    include: ['src/**/*.test.ts', 'tests/**/*.test.ts'],
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      'squad-widget-sdk/**',
      'conformance-claimant/**',
    ],
  },
})
