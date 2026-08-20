"use client";
// use-task-logs.ts 提供任务日志的 WebSocket 订阅 Hook，支持历史加载与自动重连。

import { useRef, useEffect, useCallback } from "react";
import { useAppStore } from "@/lib/store";

export interface LogMessage {
  task_id: string;
  node_id: string;
  type: "stdout" | "stderr" | "system";
  content: string;
  timestamp: number;
}

/**
 * useTaskLogs 连接指定任务的 WebSocket 日志流，
 * 并将收到的消息写入 zustand store。
 *
 * 当提供 nodeId 时，仅获取并订阅该节点的日志。
 * 切换 nodeId 会触发重新连接。
 */
export function useTaskLogs(taskId: number | null, nodeId?: string | null) {
  const wsRef = useRef<WebSocket | null>(null);
  const addLog = useAppStore((s) => s.addLog);
  const clearLogs = useAppStore((s) => s.clearLogs);
  const setLogs = useAppStore((s) => s.setLogs);

  const loadHistory = useCallback(async () => {
    if (!taskId) return;
    try {
      const token = localStorage.getItem("token");
      const headers: Record<string, string> = {};
      if (token) headers["Authorization"] = `Bearer ${token}`;
      let url = `/api/tasks/${taskId}/logs`;
      if (nodeId) url += `?node_id=${encodeURIComponent(nodeId)}`;
      const res = await fetch(url, { headers });
      if (res.ok) {
        const logs: LogMessage[] = await res.json();
        if (logs && logs.length > 0) {
          setLogs(taskId, logs);
        }
      }
    } catch {
      // 历史日志获取尽力而为；实时日志仍可正常工作
    }
  }, [taskId, nodeId, setLogs]);

  const connect = useCallback(() => {
    if (!taskId) return;

    // 关闭任何已存在的连接
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }

    const token = localStorage.getItem("token");
    if (!token) return;

    // 确定 WebSocket URL — Next.js 的 rewrites 不会代理 WebSocket，
    // 因此需要直接连接后端。
    const wsBase = process.env.NEXT_PUBLIC_WS_URL || "";
    let wsUrl: string;
    if (wsBase) {
      // 已配置显式的后端 URL（例如 ws://localhost:8080）
      const url = new URL(`/api/tasks/${taskId}/logs/ws`, wsBase);
      url.searchParams.set("token", token);
      if (nodeId) url.searchParams.set("node_id", nodeId);
      wsUrl = url.toString();
    } else {
      // 兜底：尝试同源连接（在反向代理后部署时可用）
      const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
      const host = window.location.host;
      let params = `token=${encodeURIComponent(token)}`;
      if (nodeId) params += `&node_id=${encodeURIComponent(nodeId)}`;
      wsUrl = `${proto}//${host}/api/tasks/${taskId}/logs/ws?${params}`;
    }

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onmessage = (event) => {
      try {
        const msg: LogMessage = JSON.parse(event.data);
        addLog(taskId, msg);
      } catch {
        // 忽略格式错误的消息
      }
    };

    ws.onerror = () => {
      // WebSocket 出错时会自动关闭；由 onclose 处理重连
    };

    ws.onclose = () => {
      wsRef.current = null;
      // 如果仍停留在同一任务，3 秒后自动重连
      setTimeout(() => {
        if (wsRef.current === null && taskId) {
          connect();
        }
      }, 3000);
    };
  }, [taskId, nodeId, addLog]);

  useEffect(() => {
    if (taskId) {
      // 重新加载前清除该任务的所有日志，避免重复
      clearLogs(taskId);
      // 先加载历史日志，再连接 WebSocket 获取实时更新
      loadHistory().then(() => connect());
    }

    return () => {
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [taskId, nodeId, connect, clearLogs, loadHistory]);
}
