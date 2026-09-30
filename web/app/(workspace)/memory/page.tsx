"use client";
// 共享记忆页：查看与搜索工作区共享记忆。

import React, { useState, useEffect, useCallback } from "react";
import Link from "next/link";
import { useTranslations } from "next-intl";
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
import ModalPortal from "@/lib/ModalPortal";
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
  const t = useTranslations("memory.dialog");
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
        className="bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[500px] shadow-2xl modal-content"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-line flex items-center">
          <BookOpen className="w-5 h-5 text-accent-fg mr-3" />
          <h3 className="text-lg font-bold text-fg flex-1">
            {t("title")}
          </h3>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-surface-3 rounded-lg"
          >
            <X className="w-5 h-5 text-fg-subtle" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">
              {t("titleLabel")} <span className="text-danger-fg">*</span>
            </label>
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-accent transition-colors-fast"
              placeholder={t("titlePlaceholder")}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">
              {t("contentLabel")} <span className="text-danger-fg">*</span>
            </label>
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              rows={4}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-sm text-fg-muted focus:outline-none focus:border-accent resize-none transition-colors-fast"
              placeholder={t("contentPlaceholder")}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">
              {t("typeLabel")}
            </label>
            <select
              value={type}
              onChange={(e) => setType(e.target.value)}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg text-sm focus:outline-none focus:border-accent transition-colors-fast"
            >
              <option value="architecture">{t("typeArchitecture")}</option>
              <option value="command">{t("typeCommand")}</option>
              <option value="convention">{t("typeConvention")}</option>
              <option value="decision">{t("typeDecision")}</option>
              <option value="insight">{t("typeInsight")}</option>
              <option value="environment">{t("typeEnvironment")}</option>
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">
              {t("tagsLabel")}
            </label>
            <input
              value={tags}
              onChange={(e) => setTags(e.target.value)}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg text-sm focus:outline-none focus:border-accent transition-colors-fast"
              placeholder={t("tagsPlaceholder")}
            />
          </div>
        </div>
        <div className="p-5 border-t border-line flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast"
          >
            {t("cancel")}
          </button>
          <button
            onClick={handleSave}
            disabled={!title.trim() || !content.trim() || saving}
            className="flex items-center px-5 py-2 bg-accent-strong hover:bg-accent text-fg rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
          >
            {saving ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Save className="w-4 h-4 mr-2" />
            )}
            {t("save")}
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
  const t = useTranslations("memory");
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
            className="p-2 hover:bg-surface-2 rounded-lg transition-colors-fast"
          >
            <ArrowLeft className="w-5 h-5 text-fg-subtle" />
          </Link>
          <div className="flex items-center">
            <Brain className="w-6 h-6 text-accent-fg mr-3" />
            <div>
              <h2 className="text-2xl font-bold text-fg">{t("title")}</h2>
              <p className="text-xs text-fg-subtle">
                {t("subtitle")}
              </p>
            </div>
          </div>
        </div>
        <button
          onClick={() => setShowAdd(true)}
          className="flex items-center px-4 py-2 bg-accent-strong hover:bg-accent text-fg rounded-lg text-sm font-medium transition-colors-fast btn-press"
        >
          <Plus className="w-4 h-4 mr-1.5" /> {t("add")}
        </button>
      </div>

      {/* 搜索栏 */}
      <div className="mb-6 shrink-0">
        <div className="relative">
          <Search className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-bg border border-line-strong rounded-lg pl-9 pr-4 py-2 text-sm text-fg focus:outline-none focus:border-accent transition-colors-fast"
            placeholder={t("searchPlaceholder")}
          />
        </div>
      </div>

      {/* 条目列表 */}
      <div className="flex-1 space-y-3">
        {loading ? (
          <div className="text-center py-10 text-fg-faint text-sm flex items-center justify-center gap-2">
            <Loader2 className="w-4 h-4 animate-spin" /> {t("loading")}
          </div>
        ) : entries.length === 0 ? (
          <div className="text-center py-10 text-fg-faint text-sm">
            <div className="flex flex-col items-center">
              {t("empty")}
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
              className={`bg-surface-2/30 border rounded-xl p-4 cursor-pointer transition-colors-normal card-hover ${
                selectedEntry?.id === e.id
                  ? "border-accent/50 bg-accent/5"
                  : "border-line-strong/60 hover:border-line-strong"
              }`}
            >
              <div className="flex items-start justify-between">
                <div className="flex-1 min-w-0">
                  <h4 className="text-sm font-semibold text-fg">
                    {e.title}
                  </h4>
                  <p
                    className={`text-xs text-fg-subtle mt-1 ${selectedEntry?.id === e.id ? "" : "line-clamp-2"}`}
                  >
                    {e.content}
                  </p>
                  {selectedEntry?.id === e.id && (
                    <div className="mt-3 flex items-center gap-3 text-[10px] text-fg-faint">
                      <span>{t("typeLabel", { type: e.type })}</span>
                      <span>
                        {t("createdAt", {
                          date: e.created_at
                            ? new Date(e.created_at).toLocaleDateString()
                            : "",
                        })}
                      </span>
                      <button
                        onClick={(ev) => {
                          ev.stopPropagation();
                          handleDelete(e.id);
                        }}
                        className="text-danger-fg hover:text-danger-fg flex items-center gap-1"
                      >
                        <Trash2 className="w-3 h-3" /> {t("delete")}
                      </button>
                    </div>
                  )}
                </div>
                <div className="flex items-center gap-2 ml-4 shrink-0">
                  <div className="flex flex-wrap gap-1">
                    {(e.tags || []).map((t) => (
                      <span
                        key={t}
                        className="text-[10px] px-1.5 py-0.5 bg-accent/10 text-accent-fg rounded"
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

      <div className="mt-6 pt-4 border-t border-line text-[10px] text-fg-faint">
        {t("footer", { count: entries.length })}
      </div>

      {showAdd && (
        <ModalPortal>
        <AddMemoryDialog
          onClose={() => setShowAdd(false)}
          onAdd={handleAdd}
        />
        </ModalPortal>
      )}
    </div>
  );
}
