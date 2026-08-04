import { createMDX } from 'fumadocs-mdx/next';
import type { NextConfig } from 'next';

const withMDX = createMDX();

const nextConfig: NextConfig = {
  allowedDevOrigins: ['127.0.0.1', 'localhost'],
  devIndicators: false,
  output: 'standalone',
  reactStrictMode: true,
  // 指南改版删掉了下面这些页面，但它们已经作为公开链接流传过（工单、群聊、书签）。
  // 308 到改版后承接同一主题的页面，避免老链接直接 404。
  async redirects() {
    return [
      { source: '/docs/glossary', destination: '/docs/concepts', permanent: true },
      { source: '/docs/accounts', destination: '/docs/normal-account-mode', permanent: true },
      { source: '/docs/usage', destination: '/docs/normal-check-usage', permanent: true },
      { source: '/docs/client-setup', destination: '/docs/normal-client-setup', permanent: true },
      { source: '/docs/owner-income', destination: '/docs/owner-check-income', permanent: true },
      {
        source: '/docs/operations/troubleshooting',
        destination: '/docs/operations/faq',
        permanent: true,
      },
    ];
  },
};

export default withMDX(nextConfig);
