"use client";
// 专用 Inspector(§13.3):每种内置节点一个配置组件,不做万能 JSON 表单。
// 公共区:节点名称/输入映射(agent.task)/超时与重试(适用节点)/失败端口提示。
// 表达式字段旁挂字段选择器,点击插入 {{path}},也允许手工输入路径。
import { useTranslations } from "next-intl";
import { Plus, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useAppStore } from "@/lib/store";
import type { WorkspaceIntegration } from "@/lib/api";
import { FieldPicker } from "./FieldPicker";
import { def, readSwitchRules } from "./catalog";
import type { ConditionAst, ConditionRule, TemplateNode, WorkflowTypeKey } from "./types";
import type { ValidationIssue } from "./validation";

const inputCls =
  "w-full bg-bg border border-line-strong rounded-[var(--radius-control)] px-2.5 py-1.5 text-xs text-fg focus:outline-none focus:border-primary";
const labelCls = "block text-[10px] font-semibold text-fg-faint uppercase tracking-wider mb-1";
// 卡片内联控件的紧凑输入风格(与 node_widgets 的 wInput 一致)
const compactInputCls =
  "w-full min-w-0 bg-bg border border-line-strong rounded-[var(--radius-control)] px-1.5 py-0.5 text-[10px] text-fg focus:outline-none focus:border-primary";

type Config = Record<string, unknown>;

// 不可变深路径写入:editors 以整份 config 替换提交
export function setIn(obj: unknown, path: Array<string | number>, value: unknown): unknown {
  if (path.length === 0) return value;
  const [head, ...rest] = path;
  if (Array.isArray(obj)) {
    const copy = [...obj];
    copy[head as number] = setIn(copy[head as number], rest, value);
    return copy;
  }
  const copy = { ...((obj ?? {}) as Record<string, unknown>) };
  copy[head as string] = setIn(copy[head as string], rest, value);
  return copy;
}

export function asString(v: unknown): string {
  return typeof v === "string" ? v : v === undefined || v === null ? "" : String(v);
}
export function asNumber(v: unknown): number {
  const n = typeof v === "number" ? v : Number(v);
  return Number.isFinite(n) ? n : 0;
}

// ---- 基础控件 ----

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div>
      <label className={labelCls}>{label}</label>
      {children}
      {hint && <p className="text-[9px] text-fg-faint mt-1">{hint}</p>}
    </div>
  );
}

// 文本输入的 IME 缓冲模式(ExpressionInput/WText/WArea 同此):受控值若直接绑上游,
// 每次输入触发的上游提交与整图重渲染会在拼音合成期间回写 value 打断输入法。
// 组件内维护本地草稿:聚焦时以草稿显示、合成中不上报上游;失焦与外部值对齐,
// 非聚焦时跟随外部变化(切节点/外部改动);合成结束兜底上报最终串,
// 覆盖部分浏览器 compositionend 后不再派发 input 事件的情况。
function TextInput({ value, onChange, placeholder, mono }: { value: string; onChange: (v: string) => void; placeholder?: string; mono?: boolean }) {
  const [draft, setDraft] = useState(value);
  const focusedRef = useRef(false);
  useEffect(() => {
    if (!focusedRef.current) setDraft(value);
  }, [value]);
  return (
    <input
      value={draft}
      onChange={(e) => {
        setDraft(e.target.value);
        if (!(e.nativeEvent as InputEvent).isComposing) onChange(e.target.value);
      }}
      onCompositionEnd={(e) => onChange(e.currentTarget.value)}
      onFocus={() => {
        focusedRef.current = true;
      }}
      onBlur={() => {
        focusedRef.current = false;
        setDraft(value);
      }}
      placeholder={placeholder}
      className={`${inputCls} ${mono ? "font-mono" : ""}`}
    />
  );
}

function NumInput({ value, onChange, min }: { value: number; onChange: (v: number) => void; min?: number }) {
  return (
    <input
      type="number"
      min={min ?? 0}
      value={value}
      onChange={(e) => onChange(Math.max(min ?? 0, Number(e.target.value) || 0))}
      className={inputCls}
    />
  );
}

function Select({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: Array<{ value: string; label: string }> }) {
  return (
    <select value={value} onChange={(e) => onChange(e.target.value)} className={inputCls}>
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}

// 表达式输入:输入框 + 字段选择器,选中的路径以 {{path}} 插入光标处;
// 输入框带 TextInput 同款 IME 缓冲,插入 token 时本地草稿与上游同步写入
function ExpressionInput({
  value,
  onChange,
  paths,
  placeholder,
  rows,
  mono = true,
  bare = false,
}: {
  value: string;
  onChange: (v: string) => void;
  paths: string[];
  placeholder?: string;
  rows?: number;
  mono?: boolean;
  // bare: 插入裸路径(条件 path 等吃点路径的字段);默认插入 {{ path }} 文本模板
  bare?: boolean;
}) {
  const ref = useRef<HTMLInputElement | HTMLTextAreaElement>(null);
  const [draft, setDraft] = useState(value);
  const focusedRef = useRef(false);
  useEffect(() => {
    if (!focusedRef.current) setDraft(value);
  }, [value]);
  const insert = (p: string) => {
    const token = bare ? p : `{{ ${p} }}`;
    const el = ref.current;
    if (!el) {
      const next = draft + token;
      setDraft(next);
      onChange(next);
      return;
    }
    const start = el.selectionStart ?? draft.length;
    const end = el.selectionEnd ?? start;
    const next = draft.slice(0, start) + token + draft.slice(end);
    setDraft(next);
    onChange(next);
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(start + token.length, start + token.length);
    });
  };
  const shared = {
    value: draft,
    placeholder,
    className: `${inputCls} ${mono ? "font-mono" : ""}`,
    onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      setDraft(e.target.value);
      if (!(e.nativeEvent as InputEvent).isComposing) onChange(e.target.value);
    },
    onCompositionEnd: (e: React.CompositionEvent<HTMLInputElement | HTMLTextAreaElement>) => onChange(e.currentTarget.value),
    onFocus: () => {
      focusedRef.current = true;
    },
    onBlur: () => {
      focusedRef.current = false;
      setDraft(value);
    },
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ref: ref as any,
  };
  return (
    <div className="flex items-start gap-1">
      {rows ? <textarea {...shared} rows={rows} /> : <input {...shared} />}
      <FieldPicker paths={paths} onPick={insert} />
    </div>
  );
}

// 列表型配置的行编辑器:维护本地草稿行(未填完整不写入 config),提交时整组回传;
// 行用递增 id 作 React key,避免删除中间行时输入状态串位。挂载时从 config 初始化,
// 切换节点由外部以 key 重挂载实现草稿重置。
export function RowList({
  rows,
  onRows,
  newRow,
  renderRow,
  addLabel,
}: {
  rows: Array<Record<string, string>>;
  onRows: (rows: Array<Record<string, string>>) => void;
  newRow: () => Record<string, string>;
  renderRow: (row: Record<string, string>, patch: (patch: Record<string, string>) => void) => React.ReactNode;
  addLabel: string;
}) {
  const t = useTranslations("workflows.editor.inspector");
  const nextIdRef = useRef(0);
  const [draft, setDraft] = useState<Array<{ id: number; data: Record<string, string> }>>(() =>
    rows.map((data) => ({ id: nextIdRef.current++, data }))
  );
  const commit = (next: Array<{ id: number; data: Record<string, string> }>) => {
    setDraft(next);
    onRows(next.map((r) => r.data));
  };
  return (
    <div className="space-y-1.5">
      {draft.map(({ id, data }) => (
        <div key={id} className="flex items-start gap-1">
          <div className="flex-1 min-w-0">
            {renderRow(data, (patch) => commit(draft.map((r) => (r.id === id ? { id, data: { ...data, ...patch } } : r))))}
          </div>
          <button
            type="button"
            onClick={() => commit(draft.filter((r) => r.id !== id))}
            className="mt-1 p-1 text-fg-faint hover:text-danger-fg rounded-[var(--radius-control)] hover:bg-danger/10"
            title={t("remove")}
          >
            <X className="w-3 h-3" />
          </button>
        </div>
      ))}
      <button
        type="button"
        onClick={() => commit([...draft, { id: nextIdRef.current++, data: newRow() }])}
        className="inline-flex items-center gap-1 text-[10px] text-fg-subtle hover:text-fg px-1.5 py-1 rounded-[var(--radius-control)] hover:bg-surface-2"
      >
        <Plus className="w-3 h-3" /> {addLabel}
      </button>
    </div>
  );
}

// ---- 公共区 ----

function TimeoutRetry({
  config,
  onSet,
  t,
}: {
  config: Config;
  onSet: (path: Array<string | number>, value: unknown) => void;
  t: ReturnType<typeof useTranslations>;
}) {
  const retry = (config.retry ?? {}) as Config;
  return (
    <div className="grid grid-cols-3 gap-2">
      <Field label={t("timeoutMinutes")}>
        <NumInput value={asNumber(config.timeout_minutes)} onChange={(v) => onSet(["timeout_minutes"], v)} />
      </Field>
      <Field label={t("retryAttempts")}>
        <NumInput value={asNumber(retry.max_attempts) || 1} min={1} onChange={(v) => onSet(["retry", "max_attempts"], v)} />
      </Field>
      <Field label={t("retryBackoff")}>
        <NumInput value={asNumber(retry.backoff_seconds)} onChange={(v) => onSet(["retry", "backoff_seconds"], v)} />
      </Field>
    </div>
  );
}

function InputMapEditor({ config, onSet, paths, t }: { config: Config; onSet: (path: Array<string | number>, value: unknown) => void; paths: string[]; t: ReturnType<typeof useTranslations> }) {
  const map = (config.input_map ?? {}) as Record<string, string>;
  const rows = Object.entries(map).map(([k, v]) => ({ k, v: asString(v) }));
  return (
    <Field label={t("inputMap")}>
      <RowList
        rows={rows}
        newRow={() => ({ k: "", v: "" })}
        onRows={(next) => {
          const obj: Record<string, string> = {};
          for (const r of next) if (r.k?.trim()) obj[r.k.trim()] = r.v ?? "";
          onSet(["input_map"], obj);
        }}
        addLabel={t("addRow")}
        renderRow={(row, patch) => (
          <div className="flex items-center gap-1">
            <div className="w-24 shrink-0">
              <TextInput value={row.k ?? ""} onChange={(v) => patch({ k: v })} placeholder={t("inputMapKey")} />
            </div>
            <div className="flex-1 min-w-0">
              <ExpressionInput value={row.v ?? ""} onChange={(v) => patch({ v })} paths={paths} placeholder={t("inputMapValue")} />
            </div>
          </div>
        )}
      />
    </Field>
  );
}

function DirsEditor({ label, hint, value, onChange, t }: { label: string; hint?: string; value: unknown; onChange: (v: string[]) => void; t: ReturnType<typeof useTranslations> }) {
  const text = Array.isArray(value) ? value.join(",") : asString(value);
  return (
    <Field label={label} hint={hint}>
      <TextInput
        value={text}
        onChange={(v) => onChange(v.split(",").map((s) => s.trim()).filter(Boolean))}
        placeholder={t("dirsPlaceholder")}
      />
    </Field>
  );
}

// ---- 条件编辑器(flow.condition) ----

const OPS = ["eq", "neq", "exists", "contains", "in", "gt", "gte", "lt", "lte", "matches"] as const;

function ConditionEditor({ config, onSet, paths, t }: { config: Config; onSet: (path: Array<string | number>, value: unknown) => void; paths: string[]; t: ReturnType<typeof useTranslations> }) {
  const cond = (config.condition ?? {}) as { all?: ConditionRule[]; any?: ConditionRule[] };
  const group: "all" | "any" = Array.isArray(cond.any) && cond.any.length > 0 ? "any" : "all";
  const rules = (group === "any" ? cond.any : cond.all) ?? [];
  return (
    <div className="space-y-2">
      <Field label={t("condGroup")}>
        <Select
          value={group}
          onChange={(v) => onSet(["condition"], v === "any" ? { any: rules } : { all: rules })}
          options={[
            { value: "all", label: t("condGroupAll") },
            { value: "any", label: t("condGroupAny") },
          ]}
        />
      </Field>
      <div>
        <label className={labelCls}>{t("condRules")}</label>
        <RowList
          rows={rules.map((r) => ({ path: r.path ?? "", op: r.op ?? "eq", value: r.value === undefined ? "" : asString(r.value) }))}
          newRow={() => ({ path: "", op: "eq", value: "" })}
          onRows={(next) => {
            const mapped: ConditionRule[] = next
              .filter((r) => r.path?.trim())
              .map((r) => (r.op === "exists" ? { path: r.path.trim(), op: "exists" } : { path: r.path.trim(), op: r.op as ConditionRule["op"], value: r.value }));
            onSet(["condition"], { [group]: mapped });
          }}
          addLabel={t("addRule")}
          renderRow={(row, patch) => (
            <div className="space-y-1">
              <ExpressionInput value={row.path ?? ""} onChange={(v) => patch({ path: v })} paths={paths} placeholder={t("condPath")} bare />
              <div className="flex items-center gap-1">
                <div className="w-24 shrink-0">
                  <Select value={row.op ?? "eq"} onChange={(v) => patch({ op: v })} options={OPS.map((o) => ({ value: o, label: o }))} />
                </div>
                {row.op !== "exists" && (
                  <div className="flex-1 min-w-0">
                    <TextInput value={row.value ?? ""} onChange={(v) => patch({ value: v })} placeholder={t("condValue")} mono />
                  </div>
                )}
              </div>
            </div>
          )}
        />
      </div>
    </div>
  );
}

// ---- switch 分支规则编辑器(flow.switch):每条规则 = 分支名 + 一组条件,按顺序求值 ----

interface SwitchCondDraft {
  id: number;
  path: string;
  op: string;
  value: string;
}

interface SwitchRuleDraft {
  id: number;
  branch: string;
  group: "all" | "any";
  conds: SwitchCondDraft[];
}

// 草稿行 → config.rules 结构;行不完整(空分支/空条件)也原样写入,由校验提示
function switchRuleToConfig(rule: SwitchRuleDraft): { branch: string; condition: ConditionAst } {
  const conds: ConditionRule[] = rule.conds.map((cond) =>
    cond.op === "exists" ? { path: cond.path, op: "exists" } : { path: cond.path, op: cond.op as ConditionRule["op"], value: cond.value }
  );
  return { branch: rule.branch.trim(), condition: rule.group === "any" ? { any: conds } : { all: conds } };
}

// 卡片(compact)与面板共用:compact 用单行紧凑输入,面板的 path 走带字段选择器的表达式输入
export function SwitchRulesEditor({
  config,
  onSet,
  paths,
  t,
  compact = false,
}: {
  config: Config;
  onSet: (path: Array<string | number>, value: unknown) => void;
  paths: string[];
  t: ReturnType<typeof useTranslations>;
  compact?: boolean;
}) {
  const nextIdRef = useRef(0);
  const [draft, setDraft] = useState<SwitchRuleDraft[]>(() =>
    readSwitchRules(config).map((r) => {
      const cond = (typeof r.condition === "object" && r.condition !== null ? r.condition : {}) as { all?: unknown; any?: unknown };
      const isAny = Array.isArray(cond.any) && cond.any.length > 0;
      const rules = isAny ? cond.any : cond.all;
      return {
        id: nextIdRef.current++,
        branch: r.branch,
        group: isAny ? "any" : "all",
        conds: (Array.isArray(rules) ? rules : []).map((raw) => {
          const o = (typeof raw === "object" && raw !== null ? raw : {}) as Record<string, unknown>;
          return { id: nextIdRef.current++, path: asString(o.path), op: asString(o.op) || "eq", value: asString(o.value) };
        }),
      };
    })
  );
  const commit = (next: SwitchRuleDraft[]) => {
    setDraft(next);
    onSet(["rules"], next.map(switchRuleToConfig));
  };
  const patchRule = (id: number, patch: Partial<SwitchRuleDraft>) => commit(draft.map((r) => (r.id === id ? { ...r, ...patch } : r)));
  const patchCond = (ruleId: number, condId: number, patch: Partial<SwitchCondDraft>) =>
    commit(draft.map((r) => (r.id === ruleId ? { ...r, conds: r.conds.map((c) => (c.id === condId ? { ...c, ...patch } : c)) } : r)));
  const ctl = compact ? `${compactInputCls} nodrag` : inputCls;
  const delBtn = `p-1 shrink-0 text-fg-faint hover:text-danger-fg rounded-[var(--radius-control)] hover:bg-danger/10 ${compact ? "nodrag" : ""}`;
  const addBtn = `inline-flex items-center gap-1 text-[10px] text-fg-subtle hover:text-fg px-1.5 py-1 rounded-[var(--radius-control)] hover:bg-surface-2 ${compact ? "nodrag" : ""}`;

  return (
    <div className="space-y-1.5">
      {draft.map((rule) => (
        <div key={rule.id} className={`rounded-[var(--radius-control)] border border-line ${compact ? "p-1.5 space-y-1" : "p-2 space-y-1.5"}`}>
          <div className="flex items-center gap-1">
            <input
              value={rule.branch}
              onChange={(e) => patchRule(rule.id, { branch: e.target.value })}
              placeholder="approved"
              title={t("branchNames")}
              className={`${ctl} font-mono`}
            />
            <div className={compact ? "w-20 shrink-0" : "w-28 shrink-0"}>
              <select value={rule.group} onChange={(e) => patchRule(rule.id, { group: e.target.value as "all" | "any" })} className={ctl}>
                <option value="all">{t("condGroupAll")}</option>
                <option value="any">{t("condGroupAny")}</option>
              </select>
            </div>
            <button type="button" onClick={() => commit(draft.filter((r) => r.id !== rule.id))} className={delBtn} title={t("remove")}>
              <X className="w-3 h-3" />
            </button>
          </div>
          {rule.conds.map((cond) => (
            <div key={cond.id} className="flex items-center gap-1">
              <div className="flex-1 min-w-0">
                {compact ? (
                  <div className="flex items-center gap-1">
                    <input
                      value={cond.path}
                      onChange={(e) => patchCond(rule.id, cond.id, { path: e.target.value })}
                      placeholder={t("condPath")}
                      title={t("condPath")}
                      className={`${ctl} min-w-0`}
                    />
                    {paths.length > 0 && (
                      <FieldPicker paths={paths} onPick={(p) => patchCond(rule.id, cond.id, { path: cond.path + p })} />
                    )}
                  </div>
                ) : (
                  <ExpressionInput value={cond.path} onChange={(v) => patchCond(rule.id, cond.id, { path: v })} paths={paths} placeholder={t("condPath")} bare />
                )}
              </div>
              <div className={compact ? "w-16 shrink-0" : "w-24 shrink-0"}>
                <select value={cond.op} onChange={(e) => patchCond(rule.id, cond.id, { op: e.target.value })} className={ctl}>
                  {OPS.map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </select>
              </div>
              {cond.op !== "exists" && (
                <div className={compact ? "flex-1 min-w-0 flex items-center gap-1" : "flex-1 min-w-0"}>
                  <input
                    value={cond.value}
                    onChange={(e) => patchCond(rule.id, cond.id, { value: e.target.value })}
                    placeholder={t("condValue")}
                    title={t("condValue")}
                    className={`${ctl} ${compact ? "min-w-0" : ""} font-mono`}
                  />
                  {compact && paths.length > 0 && (
                    <FieldPicker paths={paths} onPick={(p) => patchCond(rule.id, cond.id, { value: cond.value + `{{ ${p} }}` })} />
                  )}
                </div>
              )}
              <button type="button" onClick={() => patchRule(rule.id, { conds: rule.conds.filter((c) => c.id !== cond.id) })} className={delBtn} title={t("remove")}>
                <X className="w-3 h-3" />
              </button>
            </div>
          ))}
          <button
            type="button"
            onClick={() => patchRule(rule.id, { conds: [...rule.conds, { id: nextIdRef.current++, path: "", op: "eq", value: "" }] })}
            className={addBtn}
          >
            <Plus className="w-3 h-3" /> {t("addRule")}
          </button>
        </div>
      ))}
      <button type="button" onClick={() => commit([...draft, { id: nextIdRef.current++, branch: "", group: "all", conds: [] }])} className={addBtn}>
        <Plus className="w-3 h-3" /> {t("addBranch")}
      </button>
    </div>
  );
}

// ---- 类型专用区 ----

export const GITHUB_EVENTS: Record<string, string[]> = {
  issues: ["opened", "reopened", "edited", "labeled", "unlabeled", "assigned", "closed"],
  issue_comment: ["created"],
  pull_request: ["opened", "reopened", "synchronize", "closed"],
  pull_request_review: ["submitted"],
  workflow_run: ["completed"],
  push: [],
};

export const GH_OPERATIONS: Array<{ key: string; fields: string[] }> = [
  { key: "add_comment", fields: ["issue", "body"] },
  { key: "add_labels", fields: ["issue", "labels"] },
  { key: "remove_labels", fields: ["issue", "labels"] },
  { key: "assign_issue", fields: ["issue", "assignees"] },
  { key: "unassign_issue", fields: ["issue", "assignees"] },
  { key: "close_issue", fields: ["issue"] },
  { key: "reopen_issue", fields: ["issue"] },
  { key: "create_pull_request", fields: ["title", "head", "base", "body", "comment_issue"] },
  { key: "request_reviewers", fields: ["pr", "reviewers"] },
];

export const GH_FIELD_PLACEHOLDER: Record<string, string> = {
  issue: "{{ trigger.issue.number }}",
  pr: "{{ trigger.pull_request.number }}",
  head: "teammate/task-{{ task.id }}",
  base: "{{ trigger.repository.default_branch }}",
  title: "Fix Issue #{{ trigger.issue.number }}",
  body: "",
  labels: "bug,triage",
  assignees: "octocat",
  reviewers: "octocat",
  comment_issue: "{{ trigger.issue.number }}",
};

// 集成下拉选项:头部为空值默认项,其余为工作区真实集成(工程名 / 集成名)
export function integrationOptions(
  list: WorkspaceIntegration[],
  defaultLabel: string
): Array<{ value: string; label: string }> {
  return [{ value: "", label: defaultLabel }, ...list.map((i) => ({ value: i.id, label: `${i.project_name} / ${i.name}` }))];
}

function TypeSection({
  node,
  onSet,
  paths,
  t,
}: {
  node: TemplateNode;
  onSet: (path: Array<string | number>, value: unknown) => void;
  paths: string[];
  t: ReturnType<typeof useTranslations>;
}) {
  // 下拉数据一律来自工作区真实列表;无数据时显示空态提示,不用演示数据冒充
  const agents = useAppStore((s) => s.agents);
  const templates = useAppStore((s) => s.templates);
  const projects = useAppStore((s) => s.projects);
  const integrations = useAppStore((s) => s.integrations);
  const c = node.config as Config;
  switch (node.type_key) {
    case "trigger.manual":
      return <p className="text-[10px] text-fg-faint leading-relaxed">{t("triggerManualHint")}</p>;
    case "trigger.webhook":
      return <p className="text-[10px] text-fg-faint leading-relaxed">{t("webhookHint")}</p>;
    case "trigger.schedule":
      return (
        <div className="space-y-2">
          <Field label={t("scheduleInterval")}>
            <NumInput value={asNumber(c.interval_minutes)} onChange={(v) => onSet(["interval_minutes"], v)} />
          </Field>
          <Field label={t("scheduleProject")} hint={t("scheduleProjectHint")}>
            {projects.length === 0 ? (
              <p className="text-[10px] text-fg-faint leading-relaxed">{t("scheduleProjectEmpty")}</p>
            ) : (
              <Select
                value={asString(c.project_id)}
                onChange={(v) => onSet(["project_id"], v)}
                options={[{ value: "", label: t("selectPlaceholder") }, ...projects.map((p) => ({ value: p.id, label: p.name }))]}
              />
            )}
          </Field>
        </div>
      );
    case "trigger.github": {
      const event = asString(c.event) || "issues";
      const actions = Array.isArray(c.actions) ? (c.actions as string[]) : [];
      const available = GITHUB_EVENTS[event] ?? [];
      return (
        <div className="space-y-2">
          <Field label={t("githubIntegration")}>
            {integrations.length === 0 ? (
              <p className="text-[10px] text-fg-faint leading-relaxed">{t("githubIntegrationEmpty")}</p>
            ) : (
              <Select
                value={asString(c.integration_id)}
                onChange={(v) => onSet(["integration_id"], v)}
                options={integrationOptions(integrations, t("selectPlaceholder"))}
              />
            )}
          </Field>
          <Field label={t("githubEvent")}>
            <Select
              value={event}
              onChange={(v) => onSet(["event"], v)}
              options={Object.keys(GITHUB_EVENTS).map((k) => ({ value: k, label: k }))}
            />
          </Field>
          {available.length > 0 && (
            <Field label={t("githubActions")}>
              <div className="flex flex-wrap gap-1.5">
                {available.map((a) => {
                  const on = actions.includes(a);
                  return (
                    <button
                      key={a}
                      type="button"
                      onClick={() => onSet(["actions"], on ? actions.filter((x) => x !== a) : [...actions, a])}
                      className={`px-2 py-1 text-[10px] rounded-[var(--radius-control)] border transition-colors-fast ${
                        on ? "bg-primary/15 text-primary-fg border-primary/25" : "text-fg-subtle border-line hover:bg-surface-2"
                      }`}
                    >
                      {a}
                    </button>
                  );
                })}
              </div>
            </Field>
          )}
        </div>
      );
    }
    case "agent.task": {
      const assignee = (c.assignee ?? {}) as { mode?: string; agent_id?: string | null };
      const mode = assignee.mode === "specific_agent" ? "specific_agent" : "any_agent";
      return (
        <div className="space-y-3">
          <Field label={t("instruction")}>
            <ExpressionInput value={asString(c.instruction)} onChange={(v) => onSet(["instruction"], v)} paths={paths} rows={4} mono={false} />
          </Field>
          <Field label={t("assigneeMode")}>
            <Select
              value={mode}
              onChange={(v) => onSet(["assignee"], { mode: v, agent_id: v === "specific_agent" ? assignee.agent_id ?? null : null })}
              options={[
                { value: "any_agent", label: t("assigneeAny") },
                { value: "specific_agent", label: t("assigneeSpecific") },
              ]}
            />
          </Field>
          {mode === "specific_agent" && (
            <Field label={t("assigneeAgent")}>
              {agents.length === 0 ? (
                <p className="text-[10px] text-fg-faint leading-relaxed">{t("assigneeAgentEmpty")}</p>
              ) : (
                <Select
                  value={asString(assignee.agent_id)}
                  onChange={(v) => onSet(["assignee", "agent_id"], v || null)}
                  options={[{ value: "", label: t("selectPlaceholder") }, ...agents.map((a) => ({ value: a.id, label: a.name }))]}
                />
              )}
            </Field>
          )}
          <DirsEditor label={t("readonlyDirs")} hint={t("readonlyDirsHint")} value={c.readonly_dirs} onChange={(v) => onSet(["readonly_dirs"], v)} t={t} />
          <DirsEditor label={t("fullControlDirs")} hint={t("fullControlDirsHint")} value={c.full_control_dirs} onChange={(v) => onSet(["full_control_dirs"], v)} t={t} />
        </div>
      );
    }
    case "action.github": {
      const op = GH_OPERATIONS.find((o) => o.key === c.operation) ?? GH_OPERATIONS[0];
      return (
        <div className="space-y-3">
          <Field label={t("ghOperation")}>
            <Select
              value={op.key}
              onChange={(v) => {
                const next = GH_OPERATIONS.find((o) => o.key === v)!;
                // 一次性构造整份 config 后单次写入:逐字段 onSet 基于同一份旧 config,
                // 会互相覆盖(包括刚写入的 operation)
                const nextConfig: Config = { ...c, operation: v };
                for (const f of next.fields) if (nextConfig[f] === undefined && GH_FIELD_PLACEHOLDER[f]) nextConfig[f] = "";
                onSet([], nextConfig);
              }}
              options={GH_OPERATIONS.map((o) => ({ value: o.key, label: t(`ghOp.${o.key}`) }))}
            />
          </Field>
          <Field label={t("useIntegration")}>
            <Select
              value={asString(c.integration_id)}
              onChange={(v) => onSet(["integration_id"], v)}
              options={integrationOptions(integrations, t("integrationDefault"))}
            />
          </Field>
          {op.fields.map((f) => (
            <Field key={f} label={t(`ghField.${f}`)}>
              {f === "body" ? (
                <ExpressionInput value={asString(c[f])} onChange={(v) => onSet([f], v)} paths={paths} rows={3} mono={false} placeholder={GH_FIELD_PLACEHOLDER[f]} />
              ) : (
                <ExpressionInput value={asString(c[f])} onChange={(v) => onSet([f], v)} paths={paths} placeholder={GH_FIELD_PLACEHOLDER[f]} />
              )}
            </Field>
          ))}
        </div>
      );
    }
    case "action.http": {
      const headers = Object.entries((c.headers ?? {}) as Record<string, string>).map(([k, v]) => ({ k, v: asString(v) }));
      const query = Object.entries((c.query ?? {}) as Record<string, string>).map(([k, v]) => ({ k, v: asString(v) }));
      const kvOn = (key: string) => (rows: Array<Record<string, string>>) => {
        const obj: Record<string, string> = {};
        for (const r of rows) if (r.k?.trim()) obj[r.k.trim()] = r.v ?? "";
        onSet([key], obj);
      };
      return (
        <div className="space-y-3">
          <div className="grid grid-cols-[80px_1fr] gap-2">
            <Field label={t("httpMethod")}>
              <Select value={asString(c.method) || "GET"} onChange={(v) => onSet(["method"], v)} options={["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => ({ value: m, label: m }))} />
            </Field>
            <Field label={t("httpUrl")}>
              <ExpressionInput value={asString(c.url)} onChange={(v) => onSet(["url"], v)} paths={paths} placeholder="https://api.example.com/…" />
            </Field>
          </div>
          <Field label={t("httpHeaders")}>
            <RowList rows={headers} newRow={() => ({ k: "", v: "" })} onRows={kvOn("headers")} addLabel={t("addRow")} renderRow={(row, patch) => (
              <div className="flex items-center gap-1">
                <div className="w-24 shrink-0"><TextInput value={row.k ?? ""} onChange={(v) => patch({ k: v })} placeholder="Content-Type" /></div>
                <div className="flex-1 min-w-0"><ExpressionInput value={row.v ?? ""} onChange={(v) => patch({ v })} paths={paths} placeholder="application/json" /></div>
              </div>
            )} />
          </Field>
          <Field label={t("httpQuery")}>
            <RowList rows={query} newRow={() => ({ k: "", v: "" })} onRows={kvOn("query")} addLabel={t("addRow")} renderRow={(row, patch) => (
              <div className="flex items-center gap-1">
                <div className="w-24 shrink-0"><TextInput value={row.k ?? ""} onChange={(v) => patch({ k: v })} placeholder="page" /></div>
                <div className="flex-1 min-w-0"><ExpressionInput value={row.v ?? ""} onChange={(v) => patch({ v })} paths={paths} /></div>
              </div>
            )} />
          </Field>
          <Field label={t("httpBody")}>
            <ExpressionInput value={asString(c.body)} onChange={(v) => onSet(["body"], v)} paths={paths} rows={3} />
          </Field>
        </div>
      );
    }
    case "action.command": {
      const cmd = (c.command ?? {}) as { program?: unknown; args?: unknown; workdir?: unknown };
      return (
        <div className="space-y-3">
          <Field label={t("cmdProgram")}>
            <TextInput value={asString(cmd.program)} onChange={(v) => onSet(["command", "program"], v)} placeholder="go" mono />
          </Field>
          <Field label={t("cmdArgs")} hint={t("cmdArgsHint")}>
            {/* args 按空格拆分成数组编辑,{{path}} token 含空格,选择器插入时追加为独立元素保持完整 */}
            <div className="flex items-start gap-1">
              <div className="flex-1 min-w-0">
                <TextInput
                  value={Array.isArray(cmd.args) ? (cmd.args as string[]).join(" ") : ""}
                  onChange={(v) => onSet(["command", "args"], v.split(" ").filter(Boolean))}
                  placeholder="test ./..."
                  mono
                />
              </div>
              {paths.length > 0 && (
                <FieldPicker
                  paths={paths}
                  onPick={(p) => onSet(["command", "args"], [...(Array.isArray(cmd.args) ? (cmd.args as string[]) : []), `{{ ${p} }}`])}
                />
              )}
            </div>
          </Field>
          <Field label={t("cmdWorkdir")}>
            <TextInput value={asString(cmd.workdir)} onChange={(v) => onSet(["command", "workdir"], v)} placeholder="." mono />
          </Field>
        </div>
      );
    }
    case "action.create_task":
      return (
        <div className="space-y-3">
          <Field label={t("createTaskTitle")}>
            <ExpressionInput value={asString(c.title)} onChange={(v) => onSet(["title"], v)} paths={paths} mono={false} />
          </Field>
          <Field label={t("createTaskDesc")}>
            <ExpressionInput value={asString(c.description)} onChange={(v) => onSet(["description"], v)} paths={paths} rows={3} mono={false} />
          </Field>
        </div>
      );
    case "workflow.call":
      return (
        <div className="space-y-3">
          <Field label={t("callTemplate")}>
            {templates.length === 0 ? (
              <p className="text-[10px] text-fg-faint leading-relaxed">{t("callTemplateEmpty")}</p>
            ) : (
              <Select
                value={asString(c.template_id)}
                onChange={(v) => onSet(["template_id"], v || null)}
                options={[
                  { value: "", label: t("selectPlaceholder") },
                  ...templates.map((x) => ({ value: x.template.id, label: x.template.name })),
                ]}
              />
            )}
          </Field>
          <p className="text-[9px] text-fg-faint leading-relaxed">{t("callDepthHint")}</p>
        </div>
      );
    case "data.set":
      return (
        <div className="space-y-3">
          <Field label={t("setVarName")}>
            <TextInput value={asString(c.name)} onChange={(v) => onSet(["name"], v)} placeholder="target_branch" mono />
          </Field>
          <Field label={t("setVarValue")}>
            <ExpressionInput value={asString(c.value)} onChange={(v) => onSet(["value"], v)} paths={paths} />
          </Field>
        </div>
      );
    case "data.map": {
      const rows = Array.isArray(c.mappings)
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        ? (c.mappings as any[]).map((m) => ({ target: asString(m?.target), source: asString(m?.source) }))
        : [];
      return (
        <Field label={t("mapRows")}>
          <RowList
            rows={rows}
            newRow={() => ({ target: "", source: "" })}
            onRows={(next) => onSet(["mappings"], next.filter((r) => r.target?.trim()).map((r) => ({ target: r.target.trim(), source: r.source ?? "" })))}
            addLabel={t("addRow")}
            renderRow={(row, patch) => (
              <div className="flex items-center gap-1">
                <div className="w-24 shrink-0"><TextInput value={row.target ?? ""} onChange={(v) => patch({ target: v })} placeholder={t("mapTarget")} mono /></div>
                <div className="flex-1 min-w-0"><ExpressionInput value={row.source ?? ""} onChange={(v) => patch({ source: v })} paths={paths} placeholder={t("mapSource")} /></div>
              </div>
            )}
          />
        </Field>
      );
    }
    case "flow.condition":
      return <ConditionEditor config={c} onSet={onSet} paths={paths} t={t} />;
    case "flow.switch":
      return (
        <Field label={t("switchRules")} hint={t("branchHintSwitch")}>
          <SwitchRulesEditor config={c} onSet={onSet} paths={paths} t={t} />
        </Field>
      );
    case "flow.parallel": {
      const branches = Array.isArray(c.branches) ? (c.branches as string[]) : [];
      return (
        <Field label={t("branchNames")} hint={t("branchHintParallel")}>
          <RowList
            rows={branches.map((b) => ({ b }))}
            newRow={() => ({ b: "" })}
            onRows={(next) => onSet(["branches"], Array.from(new Set(next.map((r) => r.b?.trim()).filter(Boolean))))}
            addLabel={t("addBranch")}
            renderRow={(row, patch) => <TextInput value={row.b ?? ""} onChange={(v) => patch({ b: v })} placeholder="approved" mono />}
          />
        </Field>
      );
    }
    case "flow.join":
      return (
        <div className="space-y-2">
          <Field label={t("joinMode")}>
            <Select
              value={asString(c.mode) || "all"}
              onChange={(v) => onSet(["mode"], v)}
              options={[
                { value: "all", label: t("joinModeAll") },
                { value: "any", label: t("joinModeAny") },
              ]}
            />
          </Field>
          <p className="text-[9px] text-fg-faint leading-relaxed">{asString(c.mode) === "any" ? t("joinAnyHint") : t("joinAllHint")}</p>
        </div>
      );
    case "flow.wait":
      return (
        <div className="space-y-3">
          <Field label={t("waitMode")}>
            <Select
              value={asString(c.mode) || "duration"}
              onChange={(v) => onSet(["mode"], v)}
              options={[
                { value: "duration", label: t("waitModeDuration") },
                { value: "github_event", label: t("waitModeEvent") },
              ]}
            />
          </Field>
          {asString(c.mode) === "github_event" ? (
            <div className="space-y-2">
              <Field label={t("waitEvent")}>
                <Select value={asString(c.event)} onChange={(v) => onSet(["event"], v)} options={Object.keys(GITHUB_EVENTS).map((k) => ({ value: k, label: k }))} />
              </Field>
              <p className="text-[9px] text-fg-faint leading-relaxed">{t("waitFilterHint")}</p>
            </div>
          ) : (
            <Field label={t("waitMinutes")}>
              <NumInput value={asNumber(c.minutes)} onChange={(v) => onSet(["minutes"], v)} />
            </Field>
          )}
        </div>
      );
    case "human.input": {
      const fields = Array.isArray(c.fields) ? (c.fields as string[]) : [];
      return (
        <div className="space-y-3">
          <Field label={t("humanPrompt")}>
            <ExpressionInput value={asString(c.prompt)} onChange={(v) => onSet(["prompt"], v)} paths={paths} rows={3} mono={false} />
          </Field>
          <Field label={t("humanFields")} hint={t("humanFieldsHint")}>
            <RowList
              rows={fields.map((f) => ({ f }))}
              newRow={() => ({ f: "" })}
              onRows={(next) => onSet(["fields"], Array.from(new Set(next.map((r) => r.f?.trim()).filter(Boolean))))}
              addLabel={t("addRow")}
              renderRow={(row, patch) => <TextInput value={row.f ?? ""} onChange={(v) => patch({ f: v })} placeholder="approved" />}
            />
          </Field>
        </div>
      );
    }
    case "flow.end":
      return <p className="text-[10px] text-fg-faint leading-relaxed">{t("endHint")}</p>;
  }
}

// ---- Inspector 主体 ----

export function Inspector({
  node,
  issues,
  paths,
  hasFailureEdge,
  onName,
  onConfig,
  onTypeKey,
  onDelete,
  onClose,
  deletable,
}: {
  node: TemplateNode | null;
  issues: ValidationIssue[];
  paths: string[];
  hasFailureEdge: boolean;
  onName: (name: string) => void;
  onConfig: (config: Config) => void;
  onTypeKey: (typeKey: WorkflowTypeKey) => void;
  onDelete: () => void;
  onClose: () => void;
  deletable: boolean;
}) {
  const t = useTranslations("workflows.editor");
  const tIns = useTranslations("workflows.editor.inspector");
  const tIssues = useTranslations("workflows.editor.issues");
  if (!node) {
    return (
      <div className="w-80 shrink-0 border-l border-line bg-surface flex items-center justify-center p-6">
        <p className="text-xs text-fg-faint text-center">{t("inspectorEmpty")}</p>
      </div>
    );
  }
  const d = def(node.type_key);
  const Icon = d.icon;
  const fixed = !d.in_library;
  const hasFailedPort = node.type_key !== "flow.end" && !node.type_key.startsWith("trigger.");
  const showRetry = ["agent.task", "action.github", "action.http", "action.command", "action.create_task", "workflow.call"].includes(node.type_key);
  const onSet = (path: Array<string | number>, value: unknown) => onConfig(setIn(node.config, path, value) as Config);

  return (
    <div className="w-80 shrink-0 border-l border-line bg-surface overflow-y-auto p-4 space-y-3.5">
      <div className="flex items-center gap-2">
        <Icon className="w-4 h-4" style={{ color: "var(--primary)" }} />
        <div className="min-w-0">
          <div className="text-xs font-bold text-fg truncate">
            {node.type_key.startsWith("trigger.") ? t("startTitle") : t(`types.${node.type_key}.label`)}
          </div>
          <div className="text-[9px] text-fg-faint">{t(`types.${node.type_key}.desc`)}</div>
        </div>
        <button
          type="button"
          onClick={onClose}
          title={tIns("close")}
          className="ml-auto p-1 shrink-0 text-fg-faint hover:text-fg rounded-[var(--radius-control)] hover:bg-surface-2"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>

      {issues.length > 0 && (
        <div className="rounded-[var(--radius-control)] border border-danger/25 bg-danger/10 px-2.5 py-2 space-y-1">
          {issues.map((issue, i) => (
            <div key={i} className="text-[10px] text-danger-fg">
              {tIssues(issue.key, issue.params)}
            </div>
          ))}
        </div>
      )}

      <Field label={tIns("nodeName")}>
        {fixed ? (
          <TextInput value={node.name} onChange={onName} placeholder={t(`types.${node.type_key}.label`)} />
        ) : (
          <TextInput value={node.name} onChange={onName} placeholder={t("inspectorUntitled")} />
        )}
      </Field>

      {/* 开始节点:四种触发方式是同一节点下的可切换种类(数据契约仍为 4 个 type_key) */}
      {node.type_key.startsWith("trigger.") && (
        <Field label={tIns("triggerKind")}>
          <Select
            value={node.type_key}
            onChange={(v) => onTypeKey(v as WorkflowTypeKey)}
            options={(["trigger.manual", "trigger.schedule", "trigger.github", "trigger.webhook"] as const).map((k) => ({
              value: k,
              label: t(`types.${k}.label`),
            }))}
          />
        </Field>
      )}

      <TypeSection node={node} onSet={onSet} paths={paths} t={tIns} />

      {node.type_key === "agent.task" && (
        <InputMapEditor config={node.config as Config} onSet={onSet} paths={paths} t={tIns} />
      )}
      {showRetry && <TimeoutRetry config={node.config as Config} onSet={onSet} t={tIns} />}

      {hasFailedPort && (
        <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 px-2.5 py-2">
          <p className="text-[10px] text-fg-faint leading-relaxed">
            {hasFailureEdge ? tIns("failureHintConnected") : tIns("failureHintUnconnected")}
          </p>
        </div>
      )}

      {deletable && (
        <div className="pt-2 border-t border-line">
          <button
            onClick={onDelete}
            className="w-full flex items-center justify-center gap-1.5 px-2.5 py-1.5 text-xs text-danger-fg bg-danger/10 hover:bg-danger/20 border border-danger/20 rounded-[var(--radius-control)]"
          >
            <Trash2 className="w-3.5 h-3.5" /> {tIns("deleteNode")}
          </button>
        </div>
      )}
    </div>
  );
}
