import { defineConfig } from 'tsup'

export default defineConfig({
  entry: { index: 'src/index.ts' },
  format: ['esm', 'cjs'],
  dts: true,
  clean: true,
  sourcemap: true,
  splitting: false,
  treeshake: true,
  target: 'es2022',
  minify: true,
  // squad-widget-sdk 不依赖任何 runtime 包；ks-types 的常量已用字面量内联，
  // 不引入运行时依赖，从而把 gzip 体积压到 5KB 以内
  external: [],
})
