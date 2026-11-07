import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

const withNextIntl = createNextIntlPlugin("./i18n/request.ts");

// API 后端地址，默认 localhost 用于开发环境
// 生产环境通过 NEXT_PUBLIC_API_BASE_URL 指定，例如 https://api.example.com
const apiBaseUrl = process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8080";

const nextConfig: NextConfig = {
  // standalone 产物的 trace-copy 阶段需要创建软链，Windows 无特权账户会 EPERM；
  // 仅容器/CI 镜像构建（product/Dockerfile.web 注入 NEXT_BUILD_STANDALONE=1）需要它
  output: process.env.NEXT_BUILD_STANDALONE === "1" ? "standalone" : undefined,
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${apiBaseUrl}/api/:path*`,
      },
    ];
  },
};

export default withNextIntl(nextConfig);
