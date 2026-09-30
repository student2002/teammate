"use client";
// 登录/注册页：认证入口与主题切换。

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { Bot, Mail, Lock, User, Eye, EyeOff } from "lucide-react";
import api from "@/lib/api";
import ThemeSwitcher from "@/lib/themes/ThemeSwitcher";
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

  return (
    <div className="min-h-screen bg-gradient-to-br from-bg via-bg to-primary/20 flex items-center justify-center p-4">
      {/* 背景装饰 */}
      <div className="fixed inset-0 overflow-hidden pointer-events-none">
        <div className="absolute top-1/4 left-1/4 w-96 h-96 bg-primary/10 rounded-full blur-3xl animate-pulse-soft" />
        <div className="absolute bottom-1/4 right-1/4 w-96 h-96 bg-accent/10 rounded-full blur-3xl animate-pulse-soft" style={{ animationDelay: "1s" }} />
      </div>

      <div className="relative w-full max-w-md animate-fade-in-up">
        {/* 主题切换 */}
        <div className="absolute -top-12 right-0">
          <ThemeSwitcher placement="bottom" />
        </div>

        {/* Logo */}
        <div className="flex items-center justify-center mb-10">
          <div className="w-12 h-12 bg-gradient-to-br from-primary to-accent-strong rounded-xl flex items-center justify-center mr-4 shadow-lg shadow-primary/30 animate-scale-in">
            <Bot className="w-7 h-7 text-fg" />
          </div>
          <div className="animate-fade-in" style={{ animationDelay: "100ms" }}>
            <h1 className="text-2xl font-bold text-fg">Teammate</h1>
            <p className="text-xs text-fg-faint">{t("subtitle")}</p>
          </div>
        </div>

        {/* 表单卡片 */}
        <div className="bg-surface/80 backdrop-blur-xl border border-line-strong/50 rounded-2xl p-8 shadow-2xl animate-scale-in" style={{ animationDelay: "150ms" }}>
          <h2 className="text-xl font-bold text-fg mb-2">
            {mode === "login" ? t("title") : t("titleRegister")}
          </h2>
          <p className="text-sm text-fg-subtle mb-6">
            {mode === "login" ? t("desc") : t("descRegister")}
          </p>

          <form onSubmit={handleSubmit} className="space-y-4">
            {mode === "register" && (
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  {t("username")}
                </label>
                <div className="relative">
                  <User className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
                  <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    className="w-full bg-bg border border-line-strong rounded-lg pl-10 pr-4 py-3 text-fg text-sm focus:outline-none focus:border-primary transition-colors-fast"
                    placeholder={t("usernamePlaceholder")}
                  />
                </div>
              </div>
            )}

            <div>
              <label className="block text-sm font-medium text-fg-muted mb-2">
                {t("email")}
              </label>
              <div className="relative">
                <Mail className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="w-full bg-bg border border-line-strong rounded-lg pl-10 pr-4 py-3 text-fg text-sm focus:outline-none focus:border-primary transition-colors-fast"
                  placeholder="you@example.com"
                />
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium text-fg-muted mb-2">
                {t("password")}
              </label>
              <div className="relative">
                <Lock className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
                <input
                  type={showPwd ? "text" : "password"}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="w-full bg-bg border border-line-strong rounded-lg pl-10 pr-10 py-3 text-fg text-sm focus:outline-none focus:border-primary transition-colors-fast"
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
              <div className="flex items-center gap-2 bg-danger/10 border border-danger/20 rounded-lg px-4 py-3 text-sm text-danger-fg animate-fade-in">
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

            <button
              type="submit"
              disabled={loading}
              className="w-full py-3 bg-gradient-to-r from-primary-strong to-accent-strong hover:from-primary hover:to-accent text-fg rounded-lg font-semibold transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center shadow-lg shadow-primary/20 btn-press"
            >
              {loading && (
                <div className="w-5 h-5 mr-2 border-2 border-fg border-t-transparent rounded-full animate-spin" />
              )}
              {loading
                ? t("verifying")
                : mode === "login"
                  ? t("submitLogin")
                  : t("submitRegister")}
            </button>
          </form>

          {/* 切换模式 */}
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
