"use client";
// 节点卡内联参数区(ComfyUI widget 式):可配置参数以紧凑控件直接排在卡片上,
// 选中节点前即可编辑;多行结构编辑器(条件规则/键值对/映射表)仍在 Inspector。
// 控件一律带 nodrag,避免输入时拖动节点。
// Inspector 面板打开期间,该选中节点的卡片控件隐藏(卡片只显摘要),
// 避免卡片与面板两个编辑实例的草稿互踩。
import { useEffect, useRef, useState } from "react";
import { useTranslations } from "next-intl";
import { useAppStore } from "@/lib/store";
import { asNumber, asString, GITHUB_EVENTS, GH_FIELD_PLACEHOLDER, GH_OPERATIONS, integrationOptions, RowList, SwitchRulesEditor } from "./inspectors";
import { FieldPicker } from "./FieldPicker";
import type { TemplateNode, WorkflowTypeKey } from "./types";

const wInputCls =
  "w-full min-w-0 bg-bg border border-line-strong rounded-[var(--radius-control)] px-1.5 py-0.5 text-[10px] text-fg focus:outline-none focus:border-primary nodrag";

type Config = Record<string, unknown>;
type OnSet = (path: Array<string | number>, value: unknown) => void;

// WText/WArea 带 inspectors 的 TextInput 同款 IME 缓冲:本地草稿显示,合成中不上报上游,
// 失焦与外部值对齐,非聚焦时跟随外部变化;合成结束兜底上报最终串
function WText({ value, onChange, placeholder, mono }: { value: string; onChange: (v: string) => void; placeholder?: string; mono?: boolean }) {
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
      className={`${wInputCls} ${mono ? "font-mono" : ""}`}
    />
  );
}

function WNum({ value, onChange, min }: { value: number; onChange: (v: number) => void; min?: number }) {
  return <input type="number" min={min ?? 0} value={value} onChange={(e) => onChange(Math.max(min ?? 0, Number(e.target.value) || 0))} className={`${wInputCls} nodrag`} />;
}

function WSelect({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: Array<{ value: string; label: string }> }) {
  return (
    <select value={value} onChange={(e) => onChange(e.target.value)} className={`${wInputCls} nodrag`}>
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}

function WArea({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  const [draft, setDraft] = useState(value);
  const focusedRef = useRef(false);
  useEffect(() => {
    if (!focusedRef.current) setDraft(value);
  }, [value]);
  return (
    <textarea
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
      rows={2}
      className={`${wInputCls} resize-y nodrag`}
    />
  );
}

// 单行参数:左侧字段名,右侧控件(ComfyUI widget 的排布)
function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-14 shrink-0 text-[9px] text-fg-faint truncate">{label}</span>
      <div className="flex-1 min-w-0">{children}</div>
    </div>
  );
}

// 多行参数(文本域):字段名在上
function Area({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-0.5">
      <span className="block text-[9px] text-fg-faint">{label}</span>
      {children}
    </div>
  );
}

function MutedHint({ text }: { text: string }) {
  return <p className="text-[9px] text-fg-faint leading-snug">{text}</p>;
}

// 卡上表达式字段的紧凑形态:控件旁挂字段选择器。紧凑卡不追光标,点击后把
// {{ 路径 }} 追加到当前值末尾;paths 为空(运行视图等无候选场景)不挂按钮
function WithPicker({ paths, onPick, children }: { paths: string[]; onPick: (path: string) => void; children: React.ReactNode }) {
  if (paths.length === 0) return <>{children}</>;
  return (
    <div className="flex items-start gap-1">
      <div className="flex-1 min-w-0">{children}</div>
      <FieldPicker paths={paths} onPick={onPick} />
    </div>
  );
}

// 逗号分隔的字符串数组字段(目录/分支/人工字段):卡上用单行文本编辑
function JoinedList({ value, onChange, placeholder }: { value: unknown; onChange: (v: string[]) => void; placeholder?: string }) {
  const text = Array.isArray(value) ? value.join(",") : "";
  return <WText value={text} onChange={(v) => onChange(v.split(",").map((s) => s.trim()).filter(Boolean))} placeholder={placeholder} mono />;
}

// 动作 chips 与 Inspector 同一规则:只列当前事件可选的动作
function GithubActionsChips({ available, actions, onSet }: { available: string[]; actions: string[]; onSet: OnSet }) {
  return (
    <div className="flex flex-wrap gap-1">
      {available.map((a) => {
        const on = actions.includes(a);
        return (
          <button
            key={a}
            type="button"
            onClick={() => onSet(["actions"], on ? actions.filter((x) => x !== a) : [...actions, a])}
            className={`px-1.5 py-0.5 text-[9px] rounded-[var(--radius-control)] border transition-colors-fast nodrag ${
              on ? "bg-primary/15 text-primary-fg border-primary/25" : "text-fg-subtle border-line hover:bg-surface-2"
            }`}
          >
            {a}
          </button>
        );
      })}
    </div>
  );
}

// ---- 每种节点类型的卡上参数(与 Inspector 字段一一对应;多行列表除外) ----

export function CardWidgets({ node, paths, onSet }: { node: TemplateNode; paths: string[]; onSet: OnSet }) {
  const t = useTranslations("workflows.editor.inspector");
  // 下拉数据一律来自工作区真实列表,无数据显示空态提示,不用演示数据冒充
  const agents = useAppStore((s) => s.agents);
  const templates = useAppStore((s) => s.templates);
  const projects = useAppStore((s) => s.projects);
  const integrations = useAppStore((s) => s.integrations);
  const c = node.config as Config;

  switch (node.type_key) {
    case "trigger.schedule":
      return (
        <div className="space-y-1">
          <Row label={t("scheduleInterval")}>
            <WNum value={asNumber(c.interval_minutes)} onChange={(v) => onSet(["interval_minutes"], v)} />
          </Row>
          <Row label={t("scheduleProject")}>
            {projects.length === 0 ? (
              <MutedHint text={t("scheduleProjectEmpty")} />
            ) : (
              <WSelect
                value={asString(c.project_id)}
                onChange={(v) => onSet(["project_id"], v)}
                options={[{ value: "", label: t("selectPlaceholder") }, ...projects.map((p) => ({ value: p.id, label: p.name }))]}
              />
            )}
          </Row>
        </div>
      );
    case "trigger.github": {
      const event = asString(c.event) || "issues";
      const actions = Array.isArray(c.actions) ? (c.actions as string[]) : [];
      const available = GITHUB_EVENTS[event] ?? [];
      return (
        <div className="space-y-1">
          <Row label={t("githubIntegration")}>
            {integrations.length === 0 ? (
              <MutedHint text={t("githubIntegrationEmpty")} />
            ) : (
              <WSelect
                value={asString(c.integration_id)}
                onChange={(v) => onSet(["integration_id"], v)}
                options={integrationOptions(integrations, t("selectPlaceholder"))}
              />
            )}
          </Row>
          <Row label={t("githubEvent")}>
            <WSelect value={event} onChange={(v) => onSet(["event"], v)} options={Object.keys(GITHUB_EVENTS).map((k) => ({ value: k, label: k }))} />
          </Row>
          {available.length > 0 && (
            <Area label={t("githubActions")}>
              <GithubActionsChips available={available} actions={actions} onSet={onSet} />
            </Area>
          )}
        </div>
      );
    }
    case "agent.task": {
      const assignee = (c.assignee ?? {}) as { mode?: string; agent_id?: string | null };
      const mode = assignee.mode === "specific_agent" ? "specific_agent" : "any_agent";
      const retry = (c.retry ?? {}) as Config;
      return (
        <div className="space-y-1">
          <Area label={t("instruction")}>
            <WithPicker paths={paths} onPick={(p) => onSet(["instruction"], `${asString(c.instruction)}{{ ${p} }}`)}>
              <WArea value={asString(c.instruction)} onChange={(v) => onSet(["instruction"], v)} />
            </WithPicker>
          </Area>
          <Row label={t("assigneeMode")}>
            <WSelect
              value={mode}
              onChange={(v) => onSet(["assignee"], { mode: v, agent_id: v === "specific_agent" ? assignee.agent_id ?? null : null })}
              options={[
                { value: "any_agent", label: t("assigneeAny") },
                { value: "specific_agent", label: t("assigneeSpecific") },
              ]}
            />
          </Row>
          {mode === "specific_agent" &&
            (agents.length === 0 ? (
              <MutedHint text={t("assigneeAgentEmpty")} />
            ) : (
              <Row label={t("assigneeAgent")}>
                <WSelect
                  value={asString(assignee.agent_id)}
                  onChange={(v) => onSet(["assignee", "agent_id"], v || null)}
                  options={[{ value: "", label: t("selectPlaceholder") }, ...agents.map((a) => ({ value: a.id, label: a.name }))]}
                />
              </Row>
            ))}
          <Row label={t("readonlyDirs")}>
            <JoinedList value={c.readonly_dirs} onChange={(v) => onSet(["readonly_dirs"], v)} />
          </Row>
          <Row label={t("fullControlDirs")}>
            <JoinedList value={c.full_control_dirs} onChange={(v) => onSet(["full_control_dirs"], v)} />
          </Row>
          <Row label={t("timeoutMinutes")}>
            <WNum value={asNumber(c.timeout_minutes)} onChange={(v) => onSet(["timeout_minutes"], v)} />
          </Row>
          <Row label={t("retryAttempts")}>
            <WNum value={asNumber(retry.max_attempts) || 1} min={1} onChange={(v) => onSet(["retry", "max_attempts"], v)} />
          </Row>
          <Row label={t("retryBackoff")}>
            <WNum value={asNumber(retry.backoff_seconds)} onChange={(v) => onSet(["retry", "backoff_seconds"], v)} />
          </Row>
        </div>
      );
    }
    case "action.github": {
      const op = GH_OPERATIONS.find((o) => o.key === c.operation) ?? GH_OPERATIONS[0];
      return (
        <div className="space-y-1">
          <Row label={t("ghOperation")}>
            <WSelect
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
          </Row>
          <Row label={t("useIntegration")}>
            <WSelect
              value={asString(c.integration_id)}
              onChange={(v) => onSet(["integration_id"], v)}
              options={integrationOptions(integrations, t("integrationDefault"))}
            />
          </Row>
          {op.fields.map((f) =>
            f === "body" ? (
              <Area key={f} label={t(`ghField.${f}`)}>
                <WithPicker paths={paths} onPick={(p) => onSet([f], `${asString(c[f])}{{ ${p} }}`)}>
                  <WArea value={asString(c[f])} onChange={(v) => onSet([f], v)} placeholder={GH_FIELD_PLACEHOLDER[f]} />
                </WithPicker>
              </Area>
            ) : (
              <Row key={f} label={t(`ghField.${f}`)}>
                <WithPicker paths={paths} onPick={(p) => onSet([f], `${asString(c[f])}{{ ${p} }}`)}>
                  <WText value={asString(c[f])} onChange={(v) => onSet([f], v)} placeholder={GH_FIELD_PLACEHOLDER[f]} />
                </WithPicker>
              </Row>
            )
          )}
        </div>
      );
    }
    case "action.http":
      return (
        <div className="space-y-1">
          <Row label={t("httpMethod")}>
            <WSelect value={asString(c.method) || "GET"} onChange={(v) => onSet(["method"], v)} options={["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => ({ value: m, label: m }))} />
          </Row>
          <Row label={t("httpUrl")}>
            <WithPicker paths={paths} onPick={(p) => onSet(["url"], `${asString(c.url)}{{ ${p} }}`)}>
              <WText value={asString(c.url)} onChange={(v) => onSet(["url"], v)} placeholder="https://…" />
            </WithPicker>
          </Row>
          <Area label={t("httpBody")}>
            <WithPicker paths={paths} onPick={(p) => onSet(["body"], `${asString(c.body)}{{ ${p} }}`)}>
              <WArea value={asString(c.body)} onChange={(v) => onSet(["body"], v)} />
            </WithPicker>
          </Area>
        </div>
      );
    case "action.command": {
      const cmd = (c.command ?? {}) as { program?: unknown; args?: unknown; workdir?: unknown };
      return (
        <div className="space-y-1">
          <Row label={t("cmdProgram")}>
            <WText value={asString(cmd.program)} onChange={(v) => onSet(["command", "program"], v)} placeholder="go" mono />
          </Row>
          <Row label={t("cmdArgs")}>
            {/* args 按空格拆分成数组编辑,{{path}} token 含空格,选择器插入时追加为独立元素保持完整 */}
            <WithPicker
              paths={paths}
              onPick={(p) =>
                onSet(["command", "args"], [...(Array.isArray(cmd.args) ? (cmd.args as string[]) : []), `{{ ${p} }}`])
              }
            >
              <WText
                value={Array.isArray(cmd.args) ? (cmd.args as string[]).join(" ") : ""}
                onChange={(v) => onSet(["command", "args"], v.split(" ").filter(Boolean))}
                placeholder="test ./..."
                mono
              />
            </WithPicker>
          </Row>
          <Row label={t("cmdWorkdir")}>
            <WText value={asString(cmd.workdir)} onChange={(v) => onSet(["command", "workdir"], v)} placeholder="." mono />
          </Row>
        </div>
      );
    }
    case "action.create_task":
      return (
        <div className="space-y-1">
          <Row label={t("createTaskTitle")}>
            <WithPicker paths={paths} onPick={(p) => onSet(["title"], `${asString(c.title)}{{ ${p} }}`)}>
              <WText value={asString(c.title)} onChange={(v) => onSet(["title"], v)} />
            </WithPicker>
          </Row>
          <Area label={t("createTaskDesc")}>
            <WArea value={asString(c.description)} onChange={(v) => onSet(["description"], v)} />
          </Area>
        </div>
      );
    case "workflow.call":
      return (
        <div className="space-y-1">
          <Row label={t("callTemplate")}>
            {templates.length === 0 ? (
              <MutedHint text={t("callTemplateEmpty")} />
            ) : (
              <WSelect
                value={asString(c.template_id)}
                onChange={(v) => onSet(["template_id"], v || null)}
                options={[{ value: "", label: t("selectPlaceholder") }, ...templates.map((x) => ({ value: x.template.id, label: x.template.name }))]}
              />
            )}
          </Row>
        </div>
      );
    case "data.set":
      return (
        <div className="space-y-1">
          <Row label={t("setVarName")}>
            <WText value={asString(c.name)} onChange={(v) => onSet(["name"], v)} placeholder="target_branch" mono />
          </Row>
          <Row label={t("setVarValue")}>
            <WithPicker paths={paths} onPick={(p) => onSet(["value"], `${asString(c.value)}{{ ${p} }}`)}>
              <WText value={asString(c.value)} onChange={(v) => onSet(["value"], v)} />
            </WithPicker>
          </Row>
        </div>
      );
    case "flow.switch":
      return <SwitchRulesEditor config={c} onSet={onSet} paths={paths} t={t} compact />;
    case "flow.parallel": {
      const branches = Array.isArray(c.branches) ? (c.branches as string[]) : [];
      return (
        <Area label={t("branchNames")}>
          <RowList
            rows={branches.map((b) => ({ b }))}
            newRow={() => ({ b: "" })}
            onRows={(next) => onSet(["branches"], Array.from(new Set(next.map((r) => r.b?.trim()).filter(Boolean))))}
            addLabel={t("addBranch")}
            renderRow={(row, patch) => <WText value={row.b ?? ""} onChange={(v) => patch({ b: v })} placeholder="approved" mono />}
          />
        </Area>
      );
    }
    case "flow.join":
      return (
        <div className="space-y-1">
          <Row label={t("joinMode")}>
            <WSelect
              value={asString(c.mode) || "all"}
              onChange={(v) => onSet(["mode"], v)}
              options={[
                { value: "all", label: t("joinModeAll") },
                { value: "any", label: t("joinModeAny") },
              ]}
            />
          </Row>
        </div>
      );
    case "flow.wait":
      return (
        <div className="space-y-1">
          <Row label={t("waitMode")}>
            <WSelect
              value={asString(c.mode) || "duration"}
              onChange={(v) => onSet(["mode"], v)}
              options={[
                { value: "duration", label: t("waitModeDuration") },
                { value: "github_event", label: t("waitModeEvent") },
              ]}
            />
          </Row>
          {asString(c.mode) === "github_event" ? (
            <Row label={t("waitEvent")}>
              <WSelect value={asString(c.event)} onChange={(v) => onSet(["event"], v)} options={Object.keys(GITHUB_EVENTS).map((k) => ({ value: k, label: k }))} />
            </Row>
          ) : (
            <Row label={t("waitMinutes")}>
              <WNum value={asNumber(c.minutes)} onChange={(v) => onSet(["minutes"], v)} />
            </Row>
          )}
        </div>
      );
    case "human.input": {
      const fields = Array.isArray(c.fields) ? (c.fields as string[]) : [];
      return (
        <div className="space-y-1">
          <Area label={t("humanPrompt")}>
            <WithPicker paths={paths} onPick={(p) => onSet(["prompt"], `${asString(c.prompt)}{{ ${p} }}`)}>
              <WArea value={asString(c.prompt)} onChange={(v) => onSet(["prompt"], v)} />
            </WithPicker>
          </Area>
          <Area label={t("humanFields")}>
            <RowList
              rows={fields.map((f) => ({ f }))}
              newRow={() => ({ f: "" })}
              onRows={(next) => onSet(["fields"], Array.from(new Set(next.map((r) => r.f?.trim()).filter(Boolean))))}
              addLabel={t("addRow")}
              renderRow={(row, patch) => <WText value={row.f ?? ""} onChange={(v) => patch({ f: v })} placeholder="approved" />}
            />
          </Area>
        </div>
      );
    }
    default:
      return null;
  }
}

// 有卡上参数的类型返回 true(此时不再渲染静态摘要,参数本身就是展示);
// 多行结构编辑器类型(condition 规则/映射表/键值对)与无参类型保留摘要行
export function hasCardWidgets(typeKey: WorkflowTypeKey): boolean {
  return !["flow.end", "flow.condition", "data.map", "trigger.manual", "trigger.webhook"].includes(typeKey);
}
