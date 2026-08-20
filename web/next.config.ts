import type { NextConfig } from "next";

// API 后端地址，默认 localhost 用于开发环境
// 生产环境通过 NEXT_PUBLIC_API_BASE_URL 指定，例如 https://api.example.com
const apiBaseUrl = process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${apiBaseUrl}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
