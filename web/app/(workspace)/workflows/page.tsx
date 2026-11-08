"use client";
// 工作流模板管理页(整图):模板卡展示触发摘要与操作,真实拓扑在预览/编辑画布查看;
// 触发摘要从 trigger 节点 config 推导;发起任务走 POST /projects/{id}/workflows/{templateId}/runs,
// 点卡片"运行"进入整页运行子页(非弹窗、非内联):选工程、填标题与需求后发起,需求写入 input.requirement。

import React, { useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  Plus,
  Trash2,
  Rocket,
  Edit2,
  Workflow,
  LayoutTemplate,
  Database,
  Eye,
  BarChart3,
  Download,
  Upload,
  X,
  ArrowLeft,
  ChevronDown,
} from "lucide-react";
import { triggerSummary } from "@/lib/workflow/catalog";
import { useAppStore } from "@/lib/store";
import { useTranslations } from "next-intl";
import { usePermission } from "@/lib/use-permission";
import api, { ApiError } from "@/lib/api";
import PageHeader from "@/lib/ui/PageHeader";
import Button from "@/lib/ui/Button";
import EmptyState from "@/lib/ui/EmptyState";
import InlineConfirm from "@/lib/ui/InlineConfirm";
import StatusBadge from "@/lib/ui/StatusBadge";
import SectionHeader from "@/lib/ui/SectionHeader";
import { instantiateTemplate } from "@/lib/workflow/presets";
import { validateTemplate, type ValidationIssue } from "@/lib/workflow/validation";
import { IMPORT_DEFAULT_NAME, dedupeTemplateName, downloadTemplateFile, parseImportedTemplate } from "@/lib/workflow/transfer";
import type { TemplateListItem, TemplateNode } from "@/lib/workflow/types";

// ==========================================
// 模板卡
// ==========================================
function TemplateCard({
  template,
  statsExpanded,
  onToggleStats,
  onRun,
  onEdit,
  onDelete,
  onPreview,
}: {
  template: TemplateListItem;
  statsExpanded: boolean;
  onToggleStats: (templateId: string) => void;
  onRun: (template: TemplateListItem) => void;
  onEdit: (template: TemplateListItem) => void;
  onDelete: (templateId: string) => void;
  onPreview: (template: TemplateListItem) => void;
}) {
  const t = useTranslations("workflows.list");
  const tCommon = useTranslations("common");
  const tWorkflows = useTranslations("workflows");
  const { canEdit } = usePermission();
  const meta = template.template;
  const summary = triggerSummary(template.nodes);

  // 条目数据即整图({template, nodes, edges}),直接序列化下载,无需二次请求
  const handleExport = () => {
    downloadTemplateFile({
      name: meta.name,
      description: meta.description,
      enabled: meta.enabled,
      nodes: template.nodes,
      edges: template.edges,
    });
  };

  return (
    <div className="bg-surface border border-line rounded-[var(--radius-card)] p-5 flex flex-col transition-colors-fast hover:border-line-strong">
      <div className="flex items-start justify-between gap-2 mb-1.5">
        <h3 className="text-[15px] font-semibold text-fg truncate" title={meta.name}>
          {meta.name}
        </h3>
        <div className="flex items-center gap-1.5 shrink-0">
          {meta.is_builtin ? (
            <StatusBadge tone="neutral">{t("builtInTag")}</StatusBadge>
          ) : (
            <StatusBadge tone="primary">{t("customTag")}</StatusBadge>
          )}
        </div>
      </div>
      <div className="flex items-center gap-2 flex-wrap mb-3">
        <StatusBadge tone={meta.enabled ? "success" : "neutral"} dot>
          {summary ? t(`trigger.${summary.key}`, summary.params) : t("trigger.manual")} ·{" "}
          {meta.enabled ? t("enabledOn") : t("enabledOff")}
        </StatusBadge>
      </div>

      {meta.description && (
        <p className="text-xs text-fg-subtle truncate mb-3" title={meta.description}>
          {meta.description}
        </p>
      )}

      <div className="flex-1" />

      {statsExpanded && (
        <div className="mb-4 rounded-[var(--radius-control)] border border-line bg-surface-2/60 p-3">
          <div className="grid grid-cols-3 gap-2 text-center">
            <div>
              <div className="text-[10px] text-fg-faint">{t("statsRef")}</div>
              <div className="text-sm font-semibold text-fg">-</div>
              <div className="text-[10px] text-fg-faint">{t("statsTasksUnit")}</div>
            </div>
            <div>
              <div className="text-[10px] text-fg-faint">{t("statsAvgCompletion")}</div>
              <div className="text-sm font-semibold text-fg">-</div>
              <div className="text-[10px] text-fg-faint">{t("statsEndToEnd")}</div>
            </div>
            <div>
              <div className="text-[10px] text-fg-faint">{t("statsNodeCount")}</div>
              <div className="text-sm font-semibold text-fg">{template.nodes.length}</div>
              <div className="text-[10px] text-fg-faint">{t("statsStepsUnit")}</div>
            </div>
          </div>
          <p className="mt-2.5 text-[10px] text-fg-faint text-center">{t("statsNoStats")}</p>
        </div>
      )}

      <div className="mt-auto border-t border-line pt-3 flex items-center gap-2 flex-wrap">
        <Button variant="primary" size="sm" onClick={() => onRun(template)} disabled={!meta.enabled}>
          <Rocket className="w-3.5 h-3.5" /> {t("run")}
        </Button>
        <Button variant="secondary" size="sm" onClick={() => onPreview(template)}>
          <Eye className="w-3.5 h-3.5" /> {t("preview")}
        </Button>
        {!meta.is_builtin && (
          <Button variant="secondary" size="sm" onClick={() => onEdit(template)} disabled={!canEdit}>
            <Edit2 className="w-3.5 h-3.5" /> {t("edit")}
          </Button>
        )}
        <Button variant="ghost" size="sm" onClick={handleExport}>
          <Download className="w-3.5 h-3.5" /> {tWorkflows("export")}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => onToggleStats(meta.id)}
          aria-expanded={statsExpanded}
        >
          <BarChart3 className="w-3.5 h-3.5" /> {t("stats")}
        </Button>
        {!meta.is_builtin && (
          <InlineConfirm
            label={
              <>
                <Trash2 className="w-3 h-3" /> {t("delete")}
              </>
            }
            onConfirm={() => onDelete(meta.id)}
            confirmLabel={tCommon("confirm")}
            cancelLabel={tCommon("cancel")}
            disabled={!canEdit}
            className="ml-auto"
          />
        )}
      </div>
    </div>
  );
}

// ==========================================
// 运行子页(整页切换,非弹窗、非内联):选工程、填标题与需求后发起;
// 高级折叠区可传自定义入参 JSON,展开且内容非空时才校验并作为 input 提交,收起或留空视为无自定义入参
// ==========================================
function TemplateRunView({
  template,
  onBack,
}: {
  template: TemplateListItem;
  onBack: () => void;
}) {
  const t = useTranslations("workflows");
  const router = useRouter();
  const projects = useAppStore((s) => s.projects);
  const currentProjectId = useAppStore((s) => s.currentProjectId);
  const loadProjectData = useAppStore((s) => s.loadProjectData);
  const meta = template.template;

  const [projectId, setProjectId] = useState(currentProjectId ?? projects[0]?.id ?? "");
  const [title, setTitle] = useState(meta.name);
  const [requirement, setRequirement] = useState("");
  const [jsonExpanded, setJsonExpanded] = useState(false);
  const [jsonText, setJsonText] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 展开且内容非空时校验:必须是合法 JSON 对象(数组/标量/null 不视为有效入参);非法时禁用提交
  let customInput: Record<string, unknown> | null = null;
  let jsonInvalid = false;
  if (jsonExpanded && jsonText.trim()) {
    try {
      const parsed: unknown = JSON.parse(jsonText);
      if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) {
        customInput = parsed as Record<string, unknown>;
      } else {
        jsonInvalid = true;
      }
    } catch {
      jsonInvalid = true;
    }
  }

  const handleRunSubmit = async () => {
    const trimmedTitle = title.trim();
    if (!trimmedTitle) {
      setError(t("runTitleRequired"));
      return;
    }
    if (!projectId || jsonInvalid || submitting) return;
    const input: Record<string, unknown> = customInput ? { ...customInput } : {};
    const req = requirement.trim();
    if (req) input.requirement = req;
    setSubmitting(true);
    setError(null);
    try {
      const { task_id } = await api.runWorkflowTemplate(projectId, meta.id, { input, title: trimmedTitle });
      onBack();
      await loadProjectData(projectId);
      router.push(`/workflows/view?taskId=${task_id}`);
    } catch (err) {
      setSubmitting(false);
      setError(err instanceof ApiError ? err.message : t("runSubmitFailed"));
    }
  };

  return (
    <div className="page-enter h-full flex flex-col overflow-y-auto">
      <div className="flex items-center gap-2 px-4 py-2 border-b border-line bg-surface shrink-0">
        <button
          onClick={onBack}
          className="inline-flex items-center gap-1.5 h-[34px] px-2.5 text-xs text-fg-subtle hover:text-fg rounded-[var(--radius-control)] hover:bg-surface-2 transition-colors-fast focus-visible:outline-none"
        >
          <ArrowLeft className="w-3.5 h-3.5" /> {t("runBack")}
        </button>
        <div className="w-px h-5 bg-line" />
        <h1 className="text-sm font-semibold text-fg">{t("runPageTitle")}</h1>
        <StatusBadge tone="neutral">{meta.name}</StatusBadge>
      </div>

      <div className="flex-1 px-6 py-6">
        <div className="max-w-xl space-y-4">
          <div>
            <label htmlFor="run-project" className="block text-xs font-medium text-fg-muted mb-1">
              {t("runProjectLabel")} <span className="text-danger-fg">*</span>
            </label>
            <select
              id="run-project"
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
              disabled={projects.length === 0}
              className="w-full h-[34px] bg-surface border border-line-strong rounded-[var(--radius-control)] px-2.5 text-sm text-fg focus:outline-none focus:border-primary disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {projectId === "" && <option value="" disabled hidden />}
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            {projects.length === 0 && <p className="mt-1 text-xs text-danger-fg">{t("runProjectEmpty")}</p>}
          </div>

          <div>
            <label htmlFor="run-title" className="block text-xs font-medium text-fg-muted mb-1">
              {t("runTitleLabel")} <span className="text-danger-fg">*</span>
            </label>
            <input
              id="run-title"
              type="text"
              value={title}
              onChange={(e) => {
                setTitle(e.target.value);
                setError(null);
              }}
              className="w-full h-[34px] bg-surface border border-line-strong rounded-[var(--radius-control)] px-2.5 text-sm text-fg focus:outline-none focus:border-primary"
            />
          </div>

          <div>
            <label htmlFor="run-requirement" className="block text-xs font-medium text-fg-muted mb-1">
              {t("runRequirementLabel")}
            </label>
            <textarea
              id="run-requirement"
              value={requirement}
              onChange={(e) => setRequirement(e.target.value)}
              rows={4}
              placeholder={t("runRequirementPlaceholder")}
              className="w-full bg-surface border border-line-strong rounded-[var(--radius-control)] px-2.5 py-1.5 text-sm text-fg focus:outline-none focus:border-primary resize-none"
            />
          </div>

          <div className="rounded-[var(--radius-control)] border border-line bg-surface-2/60">
            <button
              type="button"
              onClick={() => setJsonExpanded((cur) => !cur)}
              aria-expanded={jsonExpanded}
              className="w-full flex items-center gap-1.5 px-2.5 py-2 text-xs font-medium text-fg-muted hover:text-fg focus-visible:outline-none"
            >
              <ChevronDown className={`w-3.5 h-3.5 transition-transform-fast ${jsonExpanded ? "rotate-180" : ""}`} />
              {t("runJsonLabel")}
            </button>
            {jsonExpanded && (
              <div className="px-2.5 pb-2.5">
                <textarea
                  value={jsonText}
                  onChange={(e) => setJsonText(e.target.value)}
                  rows={6}
                  className="w-full bg-surface border border-line-strong rounded-[var(--radius-control)] px-2.5 py-1.5 font-mono text-xs text-fg focus:outline-none focus:border-primary resize-none"
                />
                {jsonInvalid && <p className="mt-1 text-xs text-danger-fg">{t("runJsonInvalid")}</p>}
              </div>
            )}
          </div>

          {error && (
            <div
              role="alert"
              className="flex items-start gap-2 bg-danger/10 border border-danger/30 rounded-[var(--radius-control)] px-2.5 py-2 text-xs text-danger-fg"
            >
              <span>{error}</span>
            </div>
          )}

          <div className="flex justify-end">
            <Button
              variant="primary"
              onClick={handleRunSubmit}
              loading={submitting}
              disabled={!projectId || jsonInvalid}
            >
              <Rocket className="w-4 h-4" /> {t("runSubmit")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 模板列表
// ==========================================
function TemplateListView({
  templates,
  onRun,
  onCreate,
  onEdit,
  onDelete,
}: {
  templates: TemplateListItem[];
  onRun: (template: TemplateListItem) => void;
  onCreate: () => void;
  onEdit: (template: TemplateListItem) => void;
  onDelete: (templateId: string) => void;
}) {
  const t = useTranslations("workflows.list");
  const tWorkflows = useTranslations("workflows");
  const tEditor = useTranslations("workflows.editor");
  const { canEdit } = usePermission();
  const router = useRouter();
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);
  const refreshTemplates = useAppStore((s) => s.refreshTemplates);
  const builtIn = templates.filter((tpl) => tpl.template.is_builtin);
  const custom = templates.filter((tpl) => !tpl.template.is_builtin);
  const [statsTemplateId, setStatsTemplateId] = useState<string | null>(null);
  // 导入反馈:无效文件/服务端失败为文本,校验不通过为 issue 列表,成功为成功提示
  const [importError, setImportError] = useState<string | null>(null);
  const [importIssues, setImportIssues] = useState<{ issues: ValidationIssue[]; nodes: TemplateNode[] } | null>(null);
  const [importSuccess, setImportSuccess] = useState<string | null>(null);
  const [importing, setImporting] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const toggleStats = (templateId: string) => {
    setStatsTemplateId((cur) => (cur === templateId ? null : templateId));
  };

  const clearImportNotices = () => {
    setImportError(null);
    setImportIssues(null);
    setImportSuccess(null);
  };

  // 导入只新建:解析 → validateTemplate 把关 → 重名加"(导入)"后缀 → 换全新 UUID 提交 POST
  const handleImportFile = async (file: File) => {
    if (!currentWorkspaceId || importing) return;
    clearImportNotices();
    setImporting(true);
    try {
      const parsed = parseImportedTemplate(await file.text());
      if (!parsed.ok) {
        setImportError(tWorkflows("importInvalid"));
        return;
      }
      const tpl = parsed.template;
      const issues = validateTemplate(tpl.nodes, tpl.edges);
      if (issues.length > 0) {
        setImportIssues({ issues, nodes: tpl.nodes });
        return;
      }
      const existing = new Set(templates.map((item) => item.template.name));
      const finalName = dedupeTemplateName(tpl.name.trim() || IMPORT_DEFAULT_NAME, existing);
      // instantiateTemplate 把节点/边换成全新 UUID 并重写 config 内 nodes.{id}. 引用,
      // 同一工作区重复导入不会撞节点主键
      const graph = instantiateTemplate({ ...tpl, name: finalName });
      await api.createWorkflow(currentWorkspaceId, {
        name: graph.name,
        description: graph.description,
        enabled: graph.enabled,
        nodes: graph.nodes.map((n) => ({
          id: n.id,
          type_key: n.type_key,
          name: n.name,
          sort_order: n.sort_order,
          config: n.config,
          ui_config: n.ui_config,
        })),
        edges: graph.edges.map((e) => ({
          id: e.id,
          source_node_id: e.source_node_id,
          source_port: e.source_port,
          target_node_id: e.target_node_id,
          target_port: e.target_port,
          label: e.label || "",
          sort_order: 0,
          config: {},
        })),
      });
      await refreshTemplates();
      setImportSuccess(tWorkflows("importSuccess", { name: finalName }));
    } catch (err) {
      setImportError(tWorkflows("importFailed", { error: err instanceof ApiError ? err.message : String(err) }));
    } finally {
      setImporting(false);
    }
  };

  const headerActions = (
    <div className="flex items-center gap-2">
      <input
        ref={fileInputRef}
        type="file"
        accept=".json,application/json"
        className="hidden"
        onChange={(e) => {
          const file = e.target.files?.[0];
          if (file) void handleImportFile(file);
          e.target.value = "";
        }}
      />
      <Button variant="secondary" size="lg" onClick={() => fileInputRef.current?.click()} disabled={!canEdit}>
        <Upload className="w-4 h-4" /> {tWorkflows("import")}
      </Button>
      <Button variant="primary" size="lg" onClick={onCreate} disabled={!canEdit}>
        <Plus className="w-4 h-4" /> {t("page.createButton")}
      </Button>
    </div>
  );

  // 导入结果提示条(无效/失败/校验问题为错误,成功为成功态),紧跟页头展示
  const importNoticeBars = (importError || importIssues || importSuccess) && (
    <div className="px-6 pb-3">
      {importError && (
        <div role="alert" className="flex items-start gap-2 bg-danger/10 border border-danger/25 rounded-[var(--radius-control)] px-4 py-2.5 text-xs text-danger-fg">
          <span className="min-w-0 flex-1" title={importError}>{importError}</span>
          <button onClick={clearImportNotices} aria-label={tEditor("dismiss")} className="shrink-0 text-fg-subtle hover:text-fg">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}
      {importIssues && (
        <div role="alert" className="bg-danger/10 border border-danger/25 rounded-[var(--radius-control)] px-4 py-2.5 text-xs text-danger-fg space-y-1">
          {importIssues.issues.slice(0, 5).map((issue, i) => {
            const nodeName = importIssues.nodes.find((n) => n.id === issue.node_id)?.name;
            return (
              <div key={i}>
                {nodeName ? `${nodeName}: ` : ""}
                {tEditor(`issues.${issue.key}`, issue.params)}
              </div>
            );
          })}
          {importIssues.issues.length > 5 && (
            <div>{tWorkflows("importIssuesMore", { count: importIssues.issues.length - 5 })}</div>
          )}
          <div className="flex justify-end">
            <button onClick={clearImportNotices} aria-label={tEditor("dismiss")} className="shrink-0 text-fg-subtle hover:text-fg">
              <X className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      )}
      {importSuccess && (
        <div role="status" className="flex items-start gap-2 bg-success/10 border border-success/25 rounded-[var(--radius-control)] px-4 py-2.5 text-xs text-success-fg">
          <span className="min-w-0 flex-1">{importSuccess}</span>
          <button onClick={clearImportNotices} aria-label={tEditor("dismiss")} className="shrink-0 text-fg-subtle hover:text-fg">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}
    </div>
  );

  const createCard = (
    <button
      type="button"
      onClick={onCreate}
      disabled={!canEdit}
      className="border border-dashed border-line-strong rounded-[var(--radius-card)] min-h-[180px] p-6 flex flex-col items-center justify-center gap-2 text-fg-subtle hover:border-primary/50 hover:text-fg transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed disabled:pointer-events-none focus-visible:outline-none"
    >
      <LayoutTemplate className="w-8 h-8 text-fg-faint" strokeWidth={1.5} />
      <span className="text-sm font-medium">{t("page.createButton")}</span>
      {custom.length === 0 && (
        <span className="text-xs text-fg-faint">{t("noCustomTemplates")}</span>
      )}
    </button>
  );

  if (templates.length === 0) {
    return (
      <div className="page-enter h-full flex flex-col overflow-y-auto">
        <PageHeader
          title={t("page.title")}
          description={t("page.subtitle", {
            total: templates.length,
            builtIn: builtIn.length,
          })}
          actions={headerActions}
        />
        {importNoticeBars}
        <div className="px-6 pb-8 flex-1 flex items-center justify-center">
          <EmptyState
            icon={Workflow}
            title={t("emptyTitle")}
            description={t("emptyDescription")}
            action={
              <Button variant="primary" onClick={onCreate} disabled={!canEdit}>
                <Plus className="w-4 h-4" /> {t("page.createButton")}
              </Button>
            }
          />
        </div>
      </div>
    );
  }

  const renderCard = (tpl: TemplateListItem) => (
    <TemplateCard
      key={tpl.template.id}
      template={tpl}
      statsExpanded={statsTemplateId === tpl.template.id}
      onToggleStats={toggleStats}
      onRun={onRun}
      onEdit={onEdit}
      onDelete={onDelete}
      onPreview={(target) => router.push(`/workflows/view?templateId=${target.template.id}`)}
    />
  );

  return (
    <div className="page-enter h-full flex flex-col overflow-y-auto">
      <PageHeader
        title={t("page.title")}
        description={t("page.subtitle", {
          total: templates.length,
          builtIn: builtIn.length,
        })}
        actions={headerActions}
      />
      {importNoticeBars}

      <div className="px-6 pb-8 space-y-8">
        <section>
          <SectionHeader
            title={
              <span className="flex items-center gap-2">
                <LayoutTemplate className="w-4 h-4 text-fg-faint" />
                {t("customTemplates")}
              </span>
            }
            actions={<span className="text-xs text-fg-faint">{custom.length}</span>}
            className="mb-4"
          />
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
            {createCard}
            {custom.map(renderCard)}
          </div>
        </section>

        <section>
          <SectionHeader
            title={
              <span className="flex items-center gap-2">
                <Database className="w-4 h-4 text-fg-faint" />
                {t("builtInTemplates")}
              </span>
            }
            actions={<span className="text-xs text-fg-faint">{builtIn.length}</span>}
            className="mb-4"
          />
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
            {builtIn.map(renderCard)}
          </div>
        </section>
      </div>
    </div>
  );
}

// ==========================================
// 工作流模板管理主视图 (Next.js Page)
// ==========================================
export default function WorkflowsPage() {
  const router = useRouter();
  const templates = useAppStore((s) => s.templates);
  const refreshTemplates = useAppStore((s) => s.refreshTemplates);
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);
  // 子视图模式:列表 ↔ 运行子页整页切换;runTemplate 记录当前发起运行的模板
  const [subView, setSubView] = useState<"list" | "run">("list");
  const [runTemplate, setRunTemplate] = useState<TemplateListItem | null>(null);

  const handleCreateNew = () => {
    router.push("/workflows/editor");
  };

  const handleEdit = (template: TemplateListItem) => {
    if (template.template.is_builtin) return;
    router.push(`/workflows/editor?templateId=${template.template.id}`);
  };

  const handleDelete = async (templateId: string) => {
    if (!currentWorkspaceId) return;
    try {
      await api.deleteWorkflow(currentWorkspaceId, templateId);
    } catch (err) {
      console.error("Delete template failed:", err);
    }
    await refreshTemplates();
  };

  const handleRun = (template: TemplateListItem) => {
    setRunTemplate(template);
    setSubView("run");
  };

  if (subView === "run" && runTemplate) {
    return <TemplateRunView template={runTemplate} onBack={() => setSubView("list")} />;
  }

  return (
    <TemplateListView
      templates={templates}
      onRun={handleRun}
      onCreate={handleCreateNew}
      onEdit={handleEdit}
      onDelete={handleDelete}
    />
  );
}
