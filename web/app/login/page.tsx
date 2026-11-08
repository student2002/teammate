"use client";
// 登录/注册页：左侧品牌区（网格背景 + 静态协作链示意），右侧实色表单。
// 1024px 以下只保留表单与紧凑品牌头；登录、注册、校验、主题切换行为不变。

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { Mail, Lock, User, Eye, EyeOff } from "lucide-react";
import api from "@/lib/api";
import ThemeSwitcher from "@/lib/themes/ThemeSwitcher";
import BrandMark from "@/lib/product/BrandMark";
import Button from "@/lib/ui/Button";
import { useAppStore } from "@/lib/store";

export default function LoginPage() {
  const t = useTranslations("login");
  const router = useRouter();
  const setUser = useAppStore((s) => s.setUser);
  const loadInitialData = useAppStore((s) => s.loadInitialData);

  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [showPwd, setShowPwd] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email.trim() || !password.trim()) {
      setError(t("errEmpty"));
      return;
    }
    if (mode === "register" && !name.trim()) {
      setError(t("errName"));
      return;
    }
    if (password.length < 8) {
      setError(t("errPwdMin"));
      return;
    }
    if (password.length > 128) {
      setError(t("errPwdMax"));
      return;
    }
    if (!/[A-Z]/.test(password) || !/[a-z]/.test(password) || !/[0-9]/.test(password)) {
      setError(t("errPwdComplex"));
      return;
    }
    setError("");
    setLoading(true);
    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      let res: any;
      if (mode === "register") {
        res = await api.register(name, email, password);
      } else {
        res = await api.login(email, password);
      }
      localStorage.setItem("token", res.token);
      const member = res.member || {};
      setUser({
        ...member,
        role: res.role || "owner",
        workspaceId: res.workspace_id || "",
      });
      if (res.workspace_id) {
        localStorage.setItem("currentWorkspaceId", res.workspace_id);
      }
      await loadInitialData();
      router.push("/dashboard");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("failed", { defaultValue: "操作失败" }));
    } finally {
      setLoading(false);
    }
  };

  const inputClasses =
    "w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] pl-10 pr-4 text-fg text-sm focus:outline-none focus:border-primary transition-colors-fast";

  return (
    <div className="min-h-screen flex bg-bg">
      {/* 品牌区：1024px 以上显示 */}
      <div className="hidden lg:flex lg:w-[45%] min-[1440px]:w-[55%] cockpit-grid border-r border-line relative">
        <div className="m-auto max-w-md px-10 xl:px-14 py-16 animate-fade-in">
          <div className="w-12 h-12 rounded-[var(--radius-card)] bg-gradient-to-br from-primary to-accent-strong flex items-center justify-center text-fg">
            <BrandMark size={26} />
          </div>
          <h1 className="mt-6 text-3xl font-bold text-fg">Teammate</h1>
          <p className="mt-2 text-sm text-fg-subtle">{t("subtitle")}</p>
          <p className="mt-1 text-sm text-fg-subtle">{t("brandDesc")}</p>

          {/* 静态协作链示意：圆点=执行节点，菱形=审查门；纯展示，不动画 */}
          <div className="mt-12 relative" aria-hidden="true">
            <div className="absolute left-[7px] top-2 bottom-2 w-px bg-line-strong" />
            <div className="space-y-7">
              <div className="flex items-center gap-3.5 relative">
                <span className="w-[15px] h-[15px] rounded-full bg-accent ring-4 ring-accent/15 shrink-0" />
                <div>
                  <div className="text-sm font-medium text-fg">{t("chainCoding")}</div>
                  <div className="text-xs text-accent-fg mt-0.5">{t("chainCodingState")}</div>
                </div>
              </div>
              <div className="flex items-center gap-3.5 relative">
                <span className="w-[15px] h-[15px] rounded-full border-2 border-line-strong bg-surface shrink-0" />
                <div>
                  <div className="text-sm font-medium text-fg-muted">{t("chainVerify")}</div>
                  <div className="text-xs text-fg-faint mt-0.5">{t("chainVerifyState")}</div>
                </div>
              </div>
              <div className="flex items-center gap-3.5 relative">
                <span className="w-[15px] h-[15px] rotate-45 rounded-[3px] border-2 border-warning bg-surface shrink-0" />
                <div>
                  <div className="text-sm font-medium text-fg-muted">{t("chainReview")}</div>
                  <div className="text-xs text-warning-fg mt-0.5">{t("chainReviewState")}</div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* 表单区：实色表面 */}
      <div className="flex-1 bg-surface relative flex flex-col">
        <div className="absolute top-4 right-4">
          <ThemeSwitcher placement="bottom" />
        </div>

        <div className="m-auto w-full max-w-sm px-6 py-10 animate-fade-in">
          {/* 紧凑品牌头：仅窄屏显示 */}
          <div className="lg:hidden flex items-center gap-3 mb-8">
            <div className="w-9 h-9 rounded-[var(--radius-control)] bg-gradient-to-br from-primary to-accent-strong flex items-center justify-center text-fg shrink-0">
              <BrandMark size={20} />
            </div>
            <div>
              <div className="text-lg font-bold text-fg leading-tight">Teammate</div>
              <div className="text-xs text-fg-faint">{t("subtitle")}</div>
            </div>
          </div>

          <h2 className="text-xl font-bold text-fg mb-2">
            {mode === "login" ? t("title") : t("titleRegister")}
          </h2>
          <p className="text-sm text-fg-subtle mb-6">
            {mode === "login" ? t("desc") : t("descRegister")}
          </p>

          <form onSubmit={handleSubmit} className="space-y-4">
            {mode === "register" && (
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-1.5">
                  {t("username")}
                </label>
                <div className="relative">
                  <User className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
                  <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    className={inputClasses}
                    placeholder={t("usernamePlaceholder")}
                  />
                </div>
              </div>
            )}

            <div>
              <label className="block text-sm font-medium text-fg-muted mb-1.5">
                {t("email")}
              </label>
              <div className="relative">
                <Mail className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className={inputClasses}
                  placeholder="you@example.com"
                />
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium text-fg-muted mb-1.5">
                {t("password")}
              </label>
              <div className="relative">
                <Lock className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
                <input
                  type={showPwd ? "text" : "password"}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className={`${inputClasses} pr-10`}
                  placeholder={t("passwordPlaceholder")}
                />
                <button
                  type="button"
                  onClick={() => setShowPwd(!showPwd)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-fg-faint hover:text-fg-muted transition-colors-fast"
                >
                  {showPwd ? (
                    <EyeOff className="w-4 h-4" />
                  ) : (
                    <Eye className="w-4 h-4" />
                  )}
                </button>
              </div>
            </div>

            {error && (
              <div className="flex items-center gap-2 bg-danger/10 border border-danger/20 rounded-[var(--radius-control)] px-4 py-3 text-sm text-danger-fg animate-fade-in">
                <svg
                  className="w-4 h-4 shrink-0"
                  viewBox="0 0 16 16"
                  fill="currentColor"
                >
                  <path
                    fillRule="evenodd"
                    d="M8 15A7 7 0 1 0 8 1a7 7 0 0 0 0 14zm-.75-10.25a.75.75 0 0 1 1.5 0v3.5a.75.75 0 0 1-1.5 0v-3.5ZM8 11a1 1 0 1 1 0 2 1 1 0 0 1 0-2Z"
                    clipRule="evenodd"
                  />
                </svg>
                <span>{error}</span>
              </div>
            )}

            <Button
              type="submit"
              variant="primary"
              size="lg"
              loading={loading}
              className="w-full"
            >
              {loading
                ? t("verifying")
                : mode === "login"
                  ? t("submitLogin")
                  : t("submitRegister")}
            </Button>
          </form>

          <p className="mt-6 text-center text-sm text-fg-faint">
            {mode === "login" ? t("noAccount") : t("hasAccount")}
            <button
              type="button"
              onClick={() => {
                setMode((m) => (m === "login" ? "register" : "login"));
                setError("");
              }}
              className="ml-1 text-primary-fg hover:text-primary-fg font-medium transition-colors-fast"
            >
              {mode === "login" ? t("register") : t("submitLogin")}
            </button>
          </p>
        </div>
      </div>
    </div>
  );
}
