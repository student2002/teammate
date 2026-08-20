"use client";
// 共享记忆页：查看与搜索工作区共享记忆。

import React, { useState, useEffect, useCallback } from "react";
import Link from "next/link";
import {
  Brain,
  Plus,
  X,
  Search,
  BookOpen,
  Trash2,
  Save,
  Loader2,
  ArrowLeft,
} from "lucide-react";
import api from "@/lib/api";
import { useAppStore } from "@/lib/store";
import EmptyStateGuide from "@/lib/EmptyStateGuide";

// ── Types ──
interface MemoryEntry {
  id: string;
  title: string;
  content: string;
  tags: string[];
  type: string;
  created_at: string;
}

// ==========================================
// 添加知识条目弹窗
// ==========================================
function AddMemoryDialog({
  onClose,
  onAdd,
}: {
  onClose: () => void;
  onAdd: (memory: {
    title: string;
    content: string;
    tags: string[];
    type: string;
  }) => Promise<void>;
}) {
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [tags, setTags] = useState("");
  const [type, setType] = useState("insight");
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    if (!title.trim() || !content.trim()) return;
    setSaving(true);
    try {
      await onAdd({
        title: title.trim(),
        content: content.trim(),
        tags: tags
          .split(",")
          .map((t) => t.trim())
          .filter(Boolean),
        type,
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[500px] shadow-2xl modal-content"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-800 flex items-center">
          <BookOpen className="w-5 h-5 text-indigo-400 mr-3" />
          <h3 className="text-lg font-bold text-white flex-1">
            添加知识条目
          </h3>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg"
          >
            <X className="w-5 h-5 text-slate-400" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2">
              标题 <span className="text-red-400">*</span>
            </label>
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-indigo-500 transition-colors-fast"
              placeholder="如: Redis 缓存策略"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2">
              内容 <span className="text-red-400">*</span>
            </label>
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              rows={4}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-sm text-slate-300 focus:outline-none focus:border-indigo-500 resize-none transition-colors-fast"
              placeholder="记录关键技术决策和注意事项..."
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2">
              类型
            </label>
            <select
              value={type}
              onChange={(e) => setType(e.target.value)}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white text-sm focus:outline-none focus:border-indigo-500 transition-colors-fast"
            >
              <option value="architecture">架构</option>
              <option value="command">命令</option>
              <option value="convention">规范</option>
              <option value="decision">决策</option>
              <option value="insight">洞察</option>
              <option value="environment">环境</option>
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2">
              标签 (逗号分隔)
            </label>
            <input
              value={tags}
              onChange={(e) => setTags(e.target.value)}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white text-sm focus:outline-none focus:border-indigo-500 transition-colors-fast"
              placeholder="架构, 缓存, Redis"
            />
          </div>
        </div>
        <div className="p-5 border-t border-slate-800 flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors-fast"
          >
            取消
          </button>
          <button
            onClick={handleSave}
            disabled={!title.trim() || !content.trim() || saving}
            className="flex items-center px-5 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
          >
            {saving ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Save className="w-4 h-4 mr-2" />
            )}
            保存到知识库
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 共享记忆页面（从弹窗改为独立页面）
// ==========================================
export default function MemoryPage() {
  const [entries, setEntries] = useState<MemoryEntry[]>([]);
  const [search, setSearch] = useState("");
  const [showAdd, setShowAdd] = useState(false);
  const [selectedEntry, setSelectedEntry] = useState<MemoryEntry | null>(null);
  const [loading, setLoading] = useState(true);
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);

  const loadMemories = useCallback(async () => {
    if (!currentWorkspaceId) {
      setEntries([]);
      setLoading(false);
      return;
    }
    try {
      setLoading(true);
      const data = await api.listMemories({ workspace_id: currentWorkspaceId });
      setEntries((data as MemoryEntry[]) || []);
    } catch (err) {
      console.error("Failed to load memories:", err);
      setEntries([]);
    } finally {
      setLoading(false);
    }
  }, [currentWorkspaceId]);

  useEffect(() => {
    loadMemories();
  }, [loadMemories]);

  const handleSearch = useCallback(async () => {
    if (!search.trim()) {
      loadMemories();
      return;
    }
    try {
      if (!currentWorkspaceId) return;
      const data = await api.searchMemories({ q: search, workspace_id: currentWorkspaceId });
      setEntries((data as MemoryEntry[]) || []);
    } catch (err) {
      console.error("Failed to search memories:", err);
    }
  }, [search, loadMemories, currentWorkspaceId]);

  useEffect(() => {
    const timer = setTimeout(() => {
      handleSearch();
    }, 300);
    return () => clearTimeout(timer);
  }, [search, handleSearch]);

  const handleAdd = async (m: {
    title: string;
    content: string;
    tags: string[];
    type: string;
  }) => {
    if (!currentWorkspaceId) return;
    try {
      const data = {
        title: m.title,
        content: m.content,
        tags: m.tags,
        type: m.type || "insight",
        confidence: 0.5,
        verified: false,
        workspace_id: currentWorkspaceId,
      };
      const created = await api.createMemory(data);
      setEntries((prev) => [created as MemoryEntry, ...prev]);
      setShowAdd(false);
    } catch (err) {
      console.error("Failed to create memory:", err);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await api.deleteMemory(id);
      setEntries((prev) => prev.filter((e) => e.id !== id));
      if (selectedEntry?.id === id) {
        setSelectedEntry(null);
      }
    } catch (err) {
      console.error("Failed to delete memory:", err);
    }
  };

  return (
    <div className="h-full flex flex-col p-8 overflow-y-auto page-enter">
      {/* Header — 用 Link 返回代替 onClose */}
      <div className="flex justify-between items-center mb-6 shrink-0">
        <div className="flex items-center gap-4">
          <Link
            href="/dashboard"
            className="p-2 hover:bg-slate-800 rounded-lg transition-colors-fast"
          >
            <ArrowLeft className="w-5 h-5 text-slate-400" />
          </Link>
          <div className="flex items-center">
            <Brain className="w-6 h-6 text-indigo-400 mr-3" />
            <div>
              <h2 className="text-2xl font-bold text-white">共享记忆</h2>
              <p className="text-xs text-slate-400">
                团队知识库 — 节点执行时自动注入到 AI 上下文中
              </p>
            </div>
          </div>
        </div>
        <button
          onClick={() => setShowAdd(true)}
          className="flex items-center px-4 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg text-sm font-medium transition-colors-fast btn-press"
        >
          <Plus className="w-4 h-4 mr-1.5" /> 添加知识
        </button>
      </div>

      {/* 搜索栏 */}
      <div className="mb-6 shrink-0">
        <div className="relative">
          <Search className="w-4 h-4 text-slate-500 absolute left-3 top-1/2 -translate-y-1/2" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-slate-950 border border-slate-700 rounded-lg pl-9 pr-4 py-2 text-sm text-white focus:outline-none focus:border-indigo-500 transition-colors-fast"
            placeholder="搜索知识条目..."
          />
        </div>
      </div>

      {/* 条目列表 */}
      <div className="flex-1 space-y-3">
        {loading ? (
          <div className="text-center py-10 text-slate-500 text-sm flex items-center justify-center gap-2">
            <Loader2 className="w-4 h-4 animate-spin" /> 加载中...
          </div>
        ) : entries.length === 0 ? (
          <div className="text-center py-10 text-slate-500 text-sm">
            <div className="flex flex-col items-center">
              暂无知识条目
            </div>
            <div className="mt-6 w-full max-w-2xl text-left mx-auto">
              <EmptyStateGuide page="memory" />
            </div>
          </div>
        ) : (
          entries.map((e) => (
            <div
              key={e.id}
              onClick={() =>
                setSelectedEntry(
                  selectedEntry?.id === e.id ? null : e
                )
              }
              className={`bg-slate-800/30 border rounded-xl p-4 cursor-pointer transition-colors-normal card-hover ${
                selectedEntry?.id === e.id
                  ? "border-indigo-500/50 bg-indigo-500/5"
                  : "border-slate-700/60 hover:border-slate-500"
              }`}
            >
              <div className="flex items-start justify-between">
                <div className="flex-1 min-w-0">
                  <h4 className="text-sm font-semibold text-white">
                    {e.title}
                  </h4>
                  <p
                    className={`text-xs text-slate-400 mt-1 ${selectedEntry?.id === e.id ? "" : "line-clamp-2"}`}
                  >
                    {e.content}
                  </p>
                  {selectedEntry?.id === e.id && (
                    <div className="mt-3 flex items-center gap-3 text-[10px] text-slate-500">
                      <span>类型: {e.type}</span>
                      <span>
                        创建:{" "}
                        {e.created_at
                          ? new Date(e.created_at).toLocaleDateString("zh-CN")
                          : ""}
                      </span>
                      <button
                        onClick={(ev) => {
                          ev.stopPropagation();
                          handleDelete(e.id);
                        }}
                        className="text-red-400 hover:text-red-300 flex items-center gap-1"
                      >
                        <Trash2 className="w-3 h-3" /> 删除
                      </button>
                    </div>
                  )}
                </div>
                <div className="flex items-center gap-2 ml-4 shrink-0">
                  <div className="flex flex-wrap gap-1">
                    {(e.tags || []).map((t) => (
                      <span
                        key={t}
                        className="text-[10px] px-1.5 py-0.5 bg-indigo-500/10 text-indigo-300 rounded"
                      >
                        {t}
                      </span>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          ))
        )}
      </div>

      <div className="mt-6 pt-4 border-t border-slate-800 text-[10px] text-slate-500">
        共 {entries.length} 条知识 · 上下文注入优先级: 任务描述 &gt; 共享记忆
        &gt; 技能 &gt; 工程上下文 &gt; 工作区设置
      </div>

      {showAdd && (
        <AddMemoryDialog
          onClose={() => setShowAdd(false)}
          onAdd={handleAdd}
        />
      )}
    </div>
  );
}
