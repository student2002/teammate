"use client";
// 首页：处理根路径的入口跳转。

import { redirect } from "next/navigation";
import { useEffect } from "react";
import { useAppStore } from "@/lib/store";

export default function HomePage() {
  const user = useAppStore((s) => s.user);
  const loading = useAppStore((s) => s.loading);
  const loadInitialData = useAppStore((s) => s.loadInitialData);

  useEffect(() => {
    loadInitialData();
  }, [loadInitialData]);

  useEffect(() => {
    if (!loading && user) {
      redirect("/dashboard");
    }
    if (!loading && !user) {
      redirect("/login");
    }
  }, [loading, user]);

  return null;
}
