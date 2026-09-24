"use client";
// 登录/注册页：认证入口与主题切换。

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { Bot, Mail, Lock, User, Eye, EyeOff, Sun, Moon } from "lucide-react";
import api from "@/lib/api";
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
  const [theme, setTheme] = useState(() => {
    if (typeof document === "undefined") {
      return "dark";
    }
    return document.documentElement.getAttribute("data-theme") || "dark";
  });

  // 水合后读取持久化的主题，避免不一致
  useEffect(() => {
    const saved = localStorage.getItem("theme");
    if (saved) {
      setTheme(saved);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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
    <div
      data-theme={theme}
      className="min-h-screen bg-gradient-to-br from-slate-950 via-slate-950 to-blue-950/30 flex items-center justify-center p-4"
    >
      {/* 背景装饰 */}
      <div className="fixed inset-0 overflow-hidden pointer-events-none">
        <div className="absolute top-1/4 left-1/4 w-96 h-96 bg-blue-500/8 login-bg-glow-blue rounded-full blur-3xl animate-pulse-soft" />
        <div className="absolute bottom-1/4 right-1/4 w-96 h-96 bg-indigo-500/8 login-bg-glow-indigo rounded-full blur-3xl animate-pulse-soft" style={{ animationDelay: "1s" }} />
      </div>

      <div className="relative w-full max-w-md animate-fade-in-up">
        {/* 主题切换 */}
        <div className="absolute -top-12 right-0">
          <button
            onClick={() => {
              const next = theme === "dark" ? "light" : "dark";
              localStorage.setItem("theme", next);
              setTheme(next);
            }}
            className="w-9 h-9 rounded-lg bg-slate-800 hover:bg-slate-700 flex items-center justify-center transition-colors-fast btn-press"
          >
            {theme === "dark" ? (
              <Sun className="w-4 h-4 text-amber-400" />
            ) : (
              <Moon className="w-4 h-4 text-blue-400" />
            )}
          </button>
        </div>

        {/* Logo */}
        <div className="flex items-center justify-center mb-10">
          <div className="w-12 h-12 bg-gradient-to-br from-blue-500 to-indigo-600 rounded-xl flex items-center justify-center mr-4 shadow-lg shadow-blue-500/30 animate-scale-in">
            <Bot className="w-7 h-7 text-white" />
          </div>
          <div className="animate-fade-in" style={{ animationDelay: "100ms" }}>
            <h1 className="text-2xl font-bold text-white">Teammate</h1>
            <p className="text-xs text-slate-500">{t("subtitle")}</p>
          </div>
        </div>

        {/* 表单卡片 */}
        <div className="bg-slate-900/80 backdrop-blur-xl border border-slate-700/50 rounded-2xl p-8 shadow-2xl animate-scale-in" style={{ animationDelay: "150ms" }}>
          <h2 className="text-xl font-bold text-white mb-2">
            {mode === "login" ? t("title") : t("titleRegister")}
          </h2>
          <p className="text-sm text-slate-400 mb-6">
            {mode === "login" ? t("desc") : t("descRegister")}
          </p>

          <form onSubmit={handleSubmit} className="space-y-4">
            {mode === "register" && (
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-2">
                  {t("username")}
                </label>
                <div className="relative">
                  <User className="w-4 h-4 text-slate-500 absolute left-3 top-1/2 -translate-y-1/2" />
                  <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-700 rounded-lg pl-10 pr-4 py-3 text-white text-sm focus:outline-none focus:border-blue-500 transition-colors-fast"
                    placeholder={t("usernamePlaceholder")}
                  />
                </div>
              </div>
            )}

            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t("email")}
              </label>
              <div className="relative">
                <Mail className="w-4 h-4 text-slate-500 absolute left-3 top-1/2 -translate-y-1/2" />
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg pl-10 pr-4 py-3 text-white text-sm focus:outline-none focus:border-blue-500 transition-colors-fast"
                  placeholder="you@example.com"
                />
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t("password")}
              </label>
              <div className="relative">
                <Lock className="w-4 h-4 text-slate-500 absolute left-3 top-1/2 -translate-y-1/2" />
                <input
                  type={showPwd ? "text" : "password"}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg pl-10 pr-10 py-3 text-white text-sm focus:outline-none focus:border-blue-500 transition-colors-fast"
                  placeholder={t("passwordPlaceholder")}
                />
                <button
                  type="button"
                  onClick={() => setShowPwd(!showPwd)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300 transition-colors-fast"
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
              <div className="flex items-center gap-2 bg-red-500/10 border border-red-500/20 rounded-lg px-4 py-3 text-sm text-red-400 animate-fade-in">
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
              className="w-full py-3 bg-gradient-to-r from-blue-600 to-indigo-600 hover:from-blue-500 hover:to-indigo-500 text-white rounded-lg font-semibold transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center shadow-lg shadow-blue-500/20 btn-press"
            >
              {loading && (
                <div className="w-5 h-5 mr-2 border-2 border-white border-t-transparent rounded-full animate-spin" />
              )}
              {loading
                ? t("verifying")
                : mode === "login"
                  ? t("submitLogin")
                  : t("submitRegister")}
            </button>
          </form>

          {/* 切换模式 */}
          <p className="mt-6 text-center text-sm text-slate-500">
            {mode === "login" ? t("noAccount") : t("hasAccount")}
            <button
              type="button"
              onClick={() => {
                setMode((m) => (m === "login" ? "register" : "login"));
                setError("");
              }}
              className="ml-1 text-blue-400 hover:text-blue-300 font-medium transition-colors-fast"
            >
              {mode === "login" ? t("register") : t("submitLogin")}
            </button>
          </p>

        </div>
      </div>
    </div>
  );
}
