import { cookies } from "next/headers";
import { getRequestConfig } from "next-intl/server";
import { routing } from "./routing";

// 本项目不使用 next-intl 的中间件路由（页面不在 app/[locale]/ 下），
// 语言来源：NEXT_LOCALE cookie → 默认语言。
export default getRequestConfig(async ({ requestLocale }) => {
  let locale = await requestLocale;

  if (!locale || !routing.locales.includes(locale as any)) {
    const cookieLocale = (await cookies()).get("NEXT_LOCALE")?.value;
    locale =
      cookieLocale && routing.locales.includes(cookieLocale as any)
        ? cookieLocale
        : routing.defaultLocale;
  }

  return {
    locale,
    messages: (await import(`../messages/${locale}.json`)).default,
  };
});
