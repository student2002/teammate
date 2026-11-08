"use client";
// 共享记忆页：查看与搜索工作区共享记忆。条目为文档式列表（分隔线，非重卡片）；
// 新增在页头下方内联展开（同一时刻只此一个表单），删除用原位二次确认。

import { useState, useEffect, useCallback } from "react";
import { useTranslations } from "next-intl";
import {
  Brain,
  Plus,
  Search,
  BookOpen,
  Trash2,
  Save,
  Loader2,
} from "lucide-react";
import api from "@/lib/api";
import { useAppStore } from "@/lib/store";
import PageHeader from "@/lib/ui/PageHeader";
import Button from "@/lib/ui/Button";
import EmptyState from "@/lib/ui/EmptyState";
import InlineConfirm from "@/lib/ui/InlineConfirm";
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

// 内联新增表单：label 在输入框上方，文档流推开列表
function AddMemoryInline({
  onAdd,
  onClose,
}: {
  onAdd: (memory: { title: string; content: string; tags: string[]; type: string }) => Promise<void>;
  onClose: () => void;
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
          .map((s) => s.trim())
          .filter(Boolean),
        type,
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="mx-6 mb-4 border border-line rounded-[var(--radius-card)] bg-surface-2/40 p-4 space-y-3 animate-fade-in shrink-0">
      <div className="text-sm font-semibold text-fg flex items-center gap-2">
        <BookOpen className="w-4 h-4 text-accent-fg" />
        {t("title")}
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div>
          <label className="block text-xs font-medium text-fg-muted mb-1">
            {t("titleLabel")} <span className="text-danger-fg">*</span>
          </label>
          <input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-fg text-sm focus:outline-none focus:border-accent transition-colors-fast"
            placeholder={t("titlePlaceholder")}
          />
        </div>
        <div>
          <label className="block text-xs font-medium text-fg-muted mb-1">{t("typeLabel")}</label>
          <select
            value={type}
            onChange={(e) => setType(e.target.value)}
            className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-fg text-sm focus:outline-none focus:border-accent transition-colors-fast"
          >
            <option value="architecture">{t("typeArchitecture")}</option>
            <option value="command">{t("typeCommand")}</option>
            <option value="convention">{t("typeConvention")}</option>
            <option value="decision">{t("typeDecision")}</option>
            <option value="insight">{t("typeInsight")}</option>
            <option value="environment">{t("typeEnvironment")}</option>
          </select>
        </div>
        <div className="md:col-span-2">
          <label className="block text-xs font-medium text-fg-muted mb-1">
            {t("contentLabel")} <span className="text-danger-fg">*</span>
          </label>
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            rows={4}
            className="w-full bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 py-2 text-sm text-fg-muted focus:outline-none focus:border-accent resize-none transition-colors-fast"
            placeholder={t("contentPlaceholder")}
          />
        </div>
        <div>
          <label className="block text-xs font-medium text-fg-muted mb-1">{t("tagsLabel")}</label>
          <input
            value={tags}
            onChange={(e) => setTags(e.target.value)}
            className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-fg text-sm focus:outline-none focus:border-accent transition-colors-fast"
            placeholder={t("tagsPlaceholder")}
          />
        </div>
      </div>
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onClose}>
          {t("cancel")}
        </Button>
        <Button variant="primary" size="sm" loading={saving} onClick={handleSave} disabled={!title.trim() || !content.trim()}>
          <Save className="w-3.5 h-3.5 mr-1" />
          {t("save")}
        </Button>
      </div>
    </div>
  );
}

export default function MemoryPage() {
  const t = useTranslations("memory");
  const [entries, setEntries] = useState<MemoryEntry[]>([]);
  const [search, setSearch] = useState("");
  const [showAdd, setShowAdd] = useState(false);
  const [selectedEntryId, setSelectedEntryId] = useState<string | null>(null);
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

  const handleAdd = async (m: { title: string; content: string; tags: string[]; type: string }) => {
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
      if (selectedEntryId === id) {
        setSelectedEntryId(null);
      }
    } catch (err) {
      console.error("Failed to delete memory:", err);
    }
  };

  return (
    <div className="h-full flex flex-col overflow-y-auto page-enter">
      <PageHeader
        title={t("title")}
        description={t("subtitle")}
        actions={
          <Button variant="primary" onClick={() => setShowAdd(!showAdd)}>
            <Plus className="w-4 h-4 mr-1.5" /> {t("add")}
          </Button>
        }
      />

      {/* 内联新增区 */}
      {showAdd && <AddMemoryInline onAdd={handleAdd} onClose={() => setShowAdd(false)} />}

      {/* 搜索栏 */}
      <div className="px-6 mb-4 shrink-0">
        <div className="relative max-w-xl">
          <Search className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] pl-9 pr-3 text-sm text-fg focus:outline-none focus:border-accent transition-colors-fast"
            placeholder={t("searchPlaceholder")}
          />
        </div>
      </div>

      {/* 条目列表：文档式，分隔线分隔 */}
      <div className="px-6 pb-4 flex-1 min-h-0">
        {loading ? (
          <div className="text-center py-10 text-fg-faint text-sm flex items-center justify-center gap-2">
            <Loader2 className="w-4 h-4 animate-spin" /> {t("loading")}
          </div>
        ) : entries.length === 0 ? (
          <div>
            <EmptyState icon={Brain} title={t("empty")} />
            <div className="max-w-2xl mx-auto">
              <EmptyStateGuide page="memory" />
            </div>
          </div>
        ) : (
          <div className="divide-y divide-line">
            {entries.map((e) => {
              const expanded = selectedEntryId === e.id;
              return (
                <div
                  key={e.id}
                  role="button"
                  tabIndex={0}
                  onClick={() => setSelectedEntryId(expanded ? null : e.id)}
                  onKeyDown={(ev) => {
                    if (ev.key === "Enter" || ev.key === " ") {
                      ev.preventDefault();
                      setSelectedEntryId(expanded ? null : e.id);
                    }
                  }}
                  className={`py-3 cursor-pointer transition-colors-fast focus-visible:outline-none ${expanded ? "" : "hover:bg-surface-2/40"}`}
                >
                  <div className="flex items-start justify-between gap-4">
                    <div className="flex-1 min-w-0">
                      <h4 className="text-sm font-semibold text-fg">{e.title}</h4>
                      <p className={`text-xs text-fg-subtle mt-1 leading-relaxed ${expanded ? "" : "line-clamp-2"}`}>
                        {e.content}
                      </p>
                      {expanded && (
                        <div className="mt-2.5 flex items-center gap-3 text-[10px] text-fg-faint">
                          <span>{t("typeLabel", { type: e.type })}</span>
                          <span>
                            {t("createdAt", {
                              date: e.created_at ? new Date(e.created_at).toLocaleDateString() : "",
                            })}
                          </span>
                          <span onClick={(ev) => ev.stopPropagation()}>
                            <InlineConfirm
                              label={
                                <span className="flex items-center gap-1">
                                  <Trash2 className="w-3 h-3" />
                                  {t("delete")}
                                </span>
                              }
                              confirmLabel={t("delete")}
                              cancelLabel={t("dialog.cancel")}
                              onConfirm={() => handleDelete(e.id)}
                            />
                          </span>
                        </div>
                      )}
                    </div>
                    <div className="flex flex-wrap gap-1 shrink-0 max-w-[200px] justify-end">
                      {(e.tags || []).map((tag) => (
                        <span key={tag} className="text-[10px] px-1.5 py-0.5 bg-accent/10 text-accent-fg rounded">
                          {tag}
                        </span>
                      ))}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}

        <div className="mt-5 pt-3 border-t border-line text-[10px] text-fg-faint">
          {t("footer", { count: entries.length })}
        </div>
      </div>
    </div>
  );
}
