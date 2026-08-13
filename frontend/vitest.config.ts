import { defineConfig } from 'vitest/config'
import { resolve } from 'path'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      'vue-i18n': 'vue-i18n/dist/vue-i18n.runtime.esm-bundler.js'
    }
  },
  test: {
    globals: true,
    environment: 'jsdom',
    include: ['src/**/*.{test,spec}.{js,ts,jsx,tsx}'],
    exclude: ['node_modules', 'dist'],
    coverage: {
      provider: 'v8',
      ignoreEmptyLines: true,
      reporter: ['text', 'json', 'html'],
      include: ['src/**/*.{js,ts,vue}'],
      exclude: [
        'node_modules',
        'src/**/*.d.ts',
        'src/**/*.spec.ts',
        'src/**/*.test.ts',
        'src/i18n/locales/*.ts',
        'src/types/**/*.ts',
        'src/components/common/types.ts',
        'src/views/admin/ops/types.ts'
      ],
      thresholds: {
        statements: 53.58,
        branches: 67.25,
        functions: 43.21,
        lines: 53.58,
        'src/stores/auth.ts': {
          statements: 80,
          branches: 80,
          functions: 80,
          lines: 80
        },
        'src/components/payment/paymentFlow.ts': {
          statements: 80,
          branches: 80,
          functions: 80,
          lines: 80
        },
        'src/components/account/credentialsBuilder.ts': {
          statements: 80,
          branches: 80,
          functions: 80,
          lines: 80
        },
        'src/components/account-share/externalPlacement.ts': {
          statements: 80,
          branches: 80,
          functions: 80,
          lines: 80
        },
        'src/api/admin/accountShareQuota.ts': {
          statements: 80,
          branches: 80,
          functions: 80,
          lines: 80
        },
        'src/views/admin/groupPricingForm.ts': {
          statements: 80,
          branches: 80,
          functions: 80,
          lines: 80
        },
        'src/utils/accountUsageRefresh.ts': {
          statements: 80,
          branches: 80,
          functions: 80,
          lines: 80
        },
        'src/utils/registrationEmailPolicy.ts': {
          statements: 80,
          branches: 80,
          functions: 80,
          lines: 80
        },
        'src/api/admin/groupUsageSummary.ts': {
          statements: 80,
          branches: 80,
          functions: 80,
          lines: 80
        }
      }
    }
  }
})
