import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  output: 'export',
  trailingSlash: true,
  images: {
    unoptimized: true,
  },
  transpilePackages: ['xterm'],
  env: {
    NEXT_PUBLIC_HUB_URL: process.env.PUBLIC_URL || '',
  },
};

export default nextConfig;