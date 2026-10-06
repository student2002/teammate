"use client";

import React, { useState, useRef } from "react";
import { useRouter } from "next/navigation";
import {
  Plus,
  Save,
  ArrowRight,
  ArrowDown,
  Trash2,
  Rocket,
  Edit2,
  Loader2,
  Workflow,
  LayoutTemplate,
  Database,
  User,
  FolderGit2,
  FileOutput,
  Eye,
  X,
  BarChart3,
  Clock,
  GripVertical,
} from "lucide-react";
import ModalPortal from "@/lib/ModalPortal";
import { useAppStore } from "@/lib/store";
import { useTranslations } from "next-intl";
import { usePermission } from "@/lib/use-permission";
import api from "@/lib/api";
import type {
  MappedTemplate,
  MappedAgent,
  WorkflowTemplateNode,
  Project,
  WorkflowTriggerConfig,
  WorkflowTriggerType,
} from "@/lib/types";
import { mapTemplateFromApi } from "@/lib/mappers";

// ==========================================
// 模板编辑器
// ==========================================
interface TemplateEditorViewProps {
  template: MappedTemplate;
  onSave: (saved: MappedTemplate) => void;
  onCancel: () => void;
  agents: MappedAgent[];
  projects: Project[];
  saving: boolean;
}

function TemplateEditorView({
  template,
  onSave,
  onCancel,
  agents,
  projects,
  saving,
}: TemplateEditorViewProps) {
  const t = useTranslations("workflows.editor");
  const [formData, setFormData] = useState<MappedTemplate>(template);
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);
  const dragNodeRef = useRef<WorkflowTemplateNode | null>(null);

  const updateField = <K extends keyof MappedTemplate>(
    field: K,
    value: MappedTemplate[K]
  ) => setFormData((prev) => ({ ...prev, [field]: value }));

  const updateTriggerConfig = <K extends keyof WorkflowTriggerConfig>(
    field: K,
    value: WorkflowTriggerConfig[K]
  ) =>
    setFormData((prev) => ({
      ...prev,
      triggerConfig: { ...prev.triggerConfig, [field]: value },
    }));

  const addNode = () => {
    const newNode: WorkflowTemplateNode = {
      id: Date.now(),
      name: t("defaultNodeName"),
      nodeType: "standard",
      assigneeType: "any_agent",
      assigneeId: "",
      description: "",
      timeout: 60,
      maxRejectCycles: 5,
      readonlyDirs: "",
      fullControlDirs: "",
      artifact: "",
    };
    updateField("nodes", [...formData.nodes, newNode]);
  };

  const removeNode = (id: number) =>
    updateField(
      "nodes",
      formData.nodes.filter((n) => n.id !== id)
    );
  const updateNode = <K extends keyof WorkflowTemplateNode>(
    id: number,
    field: K,
    value: WorkflowTemplateNode[K]
  ) =>
    updateField(
      "nodes",
      formData.nodes.map((n) => (n.id === id ? { ...n, [field]: value } : n))
    );

  // 拖拽排序
  const handleDragStart = (e: React.DragEvent, index: number) => {
    setDragIndex(index);
    dragNodeRef.current = formData.nodes[index];
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", String(index));
  };

  const handleDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    setDragOverIndex(index);
  };

  const handleDragLeave = () => {
    setDragOverIndex(null);
  };

  const handleDrop = (e: React.DragEvent, targetIndex: number) => {
    e.preventDefault();
    if (dragIndex === null) return;
    const nodes = [...formData.nodes];
    const [moved] = nodes.splice(dragIndex, 1);
    nodes.splice(targetIndex, 0, moved);
    updateField("nodes", nodes);
    setDragIndex(null);
    setDragOverIndex(null);
  };

  const handleDragEnd = () => {
    setDragIndex(null);
    setDragOverIndex(null);
  };

  const nodeTypeLabels: Record<string, string> = {
    standard: t("nodeTypeStandard"),
    review: t("nodeTypeReview"),
    manual: t("nodeTypeManual"),
  };

  return (
    <div className="page-enter flex flex-col min-h-[calc(100vh-3.5rem)] relative bg-bg">
      <div className="p-6 border-b border-line-strong/50 bg-surface/50 backdrop-blur-md z-10 flex justify-between items-center shrink-0 sticky top-0">
        <div className="flex items-center">
          <button
            onClick={onCancel}
            className="p-2 mr-4 text-fg-subtle hover:text-fg hover:bg-surface-2 rounded-lg transition-colors-fast"
          >
            <ArrowRight className="w-5 h-5 rotate-180" />
          </button>
          <div>
            <h1 className="text-2xl font-bold text-fg mb-1">
              {t("title")}
            </h1>
            <p className="text-xs text-fg-subtle">
              {t("subtitle")}
            </p>
          </div>
        </div>
        <button
          onClick={() => onSave(formData)}
          disabled={saving}
          className={`flex items-center px-6 py-2.5 rounded-lg font-medium shadow-lg transition-colors-normal shrink-0 btn-press ${
            saving
              ? "bg-primary/50 text-fg cursor-wait"
              : "bg-primary-strong hover:bg-primary text-fg shadow-primary/20"
          }`}
        >
          {saving ? (
            <Loader2 className="w-4 h-4 mr-2 animate-spin" />
          ) : (
            <Save className="w-4 h-4 mr-2" />
          )}
          {saving ? t("saving") : t("saveToLibrary")}
        </button>
      </div>

      <div className="px-8 py-12 flex justify-center pb-24">
        <div className="w-full max-w-4xl">
          {/* 基础信息 */}
          <div className="mb-10 bg-surface-2/40 border border-line-strong/60 rounded-xl p-6">
            <div className="mb-4">
              <label className="block text-sm font-medium text-fg-subtle mb-2">
                {t("nameLabel")}
              </label>
              <input
                type="text"
                value={formData.name}
                onChange={(e) => updateField("name", e.target.value)}
                className="w-full bg-surface border border-line-strong rounded-lg px-4 py-2.5 text-fg font-semibold text-lg focus:outline-none focus:border-primary"
              />
            </div>
            <div className="grid grid-cols-2 gap-5 mb-4">
              <div>
                <label className="block text-sm font-medium text-fg-subtle mb-2">
                  {t("descLabel")}
                </label>
                <textarea
                  value={formData.description}
                  onChange={(e) => updateField("description", e.target.value)}
                  rows={2}
                  className="w-full bg-surface border border-line-strong rounded-lg px-4 py-2.5 text-fg-muted text-sm focus:outline-none focus:border-primary resize-none"
                />
              </div>
            </div>
          </div>

          <div className="mb-10 bg-surface-2/40 border border-line-strong/60 rounded-xl p-6">
            <div className="flex items-center justify-between mb-5">
              <h3 className="text-lg font-bold text-fg">{t("triggerTitle")}</h3>
              <label className="flex items-center gap-2 text-sm text-fg-muted">
                <input
                  type="checkbox"
                  checked={formData.triggerEnabled}
                  onChange={(e) => updateField("triggerEnabled", e.target.checked)}
                  className="h-4 w-4 rounded border-line-strong bg-bg text-primary-strong focus:ring-primary"
                />
                {t("triggerEnable")}
              </label>
            </div>
            <div className="grid grid-cols-3 gap-3 mb-5">
              {[
                { value: "manual", label: t("triggerManual"), icon: User },
                { value: "schedule", label: t("triggerSchedule"), icon: Clock },
                { value: "github_issue", label: "GitHub Issue", icon: FolderGit2 },
              ].map((item) => {
                const Icon = item.icon;
                const active = formData.triggerType === item.value;
                return (
                  <button
                    key={item.value}
                    type="button"
                    onClick={() =>
                      updateField("triggerType", item.value as WorkflowTriggerType)
                    }
                    className={`flex items-center justify-center gap-2 rounded-lg border px-3 py-3 text-sm font-medium transition-colors ${
                      active
                        ? "border-primary bg-primary/10 text-primary-fg"
                        : "border-line-strong bg-bg text-fg-subtle hover:border-line-strong"
                    }`}
                  >
                    <Icon className="h-4 w-4" />
                    {item.label}
                  </button>
                );
              })}
            </div>

            {formData.triggerType === "schedule" && (
              <div className="grid grid-cols-2 gap-4">
                <select
                  value={formData.triggerConfig.projectId || ""}
                  onChange={(e) => updateTriggerConfig("projectId", e.target.value)}
                  className="w-full bg-bg border border-line-strong rounded-lg px-3 py-2.5 text-sm text-fg-muted focus:outline-none focus:border-primary"
                >
                  <option value="">{t("selectProject")}</option>
                  {projects.map((project) => (
                    <option key={project.id} value={project.id}>
                      {project.name}
                    </option>
                  ))}
                </select>
                <input
                  type="number"
                  min={1}
                  value={formData.triggerConfig.intervalMinutes || 60}
                  onChange={(e) =>
                    updateTriggerConfig("intervalMinutes", parseInt(e.target.value) || 60)
                  }
                  className="w-full bg-bg border border-line-strong rounded-lg px-3 py-2.5 text-sm text-fg focus:outline-none focus:border-primary"
                  placeholder={t("intervalPlaceholder")}
                />
                <input
                  type="datetime-local"
                  value={formData.nextRunAt ? formData.nextRunAt.slice(0, 16) : ""}
                  onChange={(e) =>
                    updateField(
                      "nextRunAt",
                      e.target.value ? new Date(e.target.value).toISOString() : ""
                    )
                  }
                  className="w-full bg-bg border border-line-strong rounded-lg px-3 py-2.5 text-sm text-fg focus:outline-none focus:border-primary"
                />
                <input
                  type="text"
                  value={formData.triggerConfig.title || ""}
                  onChange={(e) => updateTriggerConfig("title", e.target.value)}
                  className="bg-bg border border-line-strong rounded-lg px-3 py-2.5 text-sm text-fg focus:outline-none focus:border-primary"
                  placeholder={t("taskTitlePlaceholder")}
                />
                <input
                  type="text"
                  value={formData.triggerConfig.description || ""}
                  onChange={(e) => updateTriggerConfig("description", e.target.value)}
                  className="bg-bg border border-line-strong rounded-lg px-3 py-2.5 text-sm text-fg focus:outline-none focus:border-primary"
                  placeholder={t("taskDescPlaceholder")}
                />
              </div>
            )}

            {formData.triggerType === "github_issue" && (
              <div className="grid grid-cols-2 gap-4">
                <select
                  value={formData.triggerConfig.projectId || ""}
                  onChange={(e) => updateTriggerConfig("projectId", e.target.value)}
                  className="w-full bg-bg border border-line-strong rounded-lg px-3 py-2.5 text-sm text-fg-muted focus:outline-none focus:border-primary"
                >
                  <option value="">{t("selectProject")}</option>
                  {projects.map((project) => (
                    <option key={project.id} value={project.id}>
                      {project.name}
                    </option>
                  ))}
                </select>
                <input
                  type="text"
                  value={formData.triggerConfig.repoOwner || ""}
                  onChange={(e) => updateTriggerConfig("repoOwner", e.target.value)}
                  className="bg-bg border border-line-strong rounded-lg px-3 py-2.5 text-sm text-fg focus:outline-none focus:border-primary"
                  placeholder={t("repoOwnerPlaceholder")}
                />
                <input
                  type="text"
                  value={formData.triggerConfig.repoName || ""}
                  onChange={(e) => updateTriggerConfig("repoName", e.target.value)}
                  className="bg-bg border border-line-strong rounded-lg px-3 py-2.5 text-sm text-fg focus:outline-none focus:border-primary"
                  placeholder={t("repoNamePlaceholder")}
                />
                <input
                  type="password"
                  value={formData.triggerConfig.secret || ""}
                  onChange={(e) => updateTriggerConfig("secret", e.target.value)}
                  className="bg-bg border border-line-strong rounded-lg px-3 py-2.5 text-sm text-fg focus:outline-none focus:border-primary"
                  placeholder="Webhook Secret"
                />
              </div>
            )}
          </div>

          <h3 className="text-lg font-bold text-fg mb-6 border-b border-line pb-2">
            {t("nodeOrchestration")}
          </h3>

          <div className="relative pl-2">
            <div className="absolute left-[35px] top-6 bottom-0 w-0.5 bg-surface-2 z-0" />

            {formData.nodes.map((node, index) => {
              const isDragging = dragIndex === index;
              const isDragOver = dragOverIndex === index && dragIndex !== index;

              return (
                <div
                  key={node.id}
                  draggable
                  onDragStart={(e) => handleDragStart(e, index)}
                  onDragOver={(e) => handleDragOver(e, index)}
                  onDragLeave={handleDragLeave}
                  onDrop={(e) => handleDrop(e, index)}
                  onDragEnd={handleDragEnd}
                  className={`relative z-10 flex items-start group transition-all animate-fade-in-up ${
                    isDragging ? "opacity-40 scale-95" : ""
                  } ${isDragOver ? "translate-y-2" : ""}`}
                >
                  <div className="flex flex-col items-center mr-6 mt-4">
                    <div
                      className={`w-14 h-14 rounded-full border-2 flex items-center justify-center font-bold shadow-xl transition-colors-fast ${
                        isDragOver
                            ? "bg-primary-strong/30 border-primary-fg text-primary-fg"
                            : "bg-surface border-line-strong text-fg-subtle group-hover:border-primary/50"
                      }`}
                    >
                      {index + 1}
                    </div>
                    {index < formData.nodes.length - 1 && (
                      <ArrowDown className="w-5 h-5 text-fg-faint my-3" />
                    )}
                  </div>

                  <div
                    className={`flex-1 rounded-xl p-6 mb-6 transition-all bg-surface-2/40 border hover:shadow-lg ${
                      isDragOver
                          ? "border-primary/50 shadow-primary/10"
                          : "border-line-strong/60 hover:border-line-strong"
                    } ${isDragging ? "border-dashed border-line-strong" : ""}`}
                  >
                    {false ? (
                      <div>
                        <div className="flex items-center mb-4">
                          <h3 className="text-xl font-bold text-accent-fg flex items-center">
                            <User className="w-5 h-5 mr-2 text-accent-fg" />{" "}
                            {t("firstNodeLocked")}
                          </h3>
                        </div>
                        <p className="text-sm text-fg-subtle">
                          {t("firstNodeHint")}
                        </p>
                      </div>
                    ) : (
                      <div>
                        <div className="flex justify-between items-start mb-5">
                          <div className="flex-1 flex items-start gap-2">
                            <div
                              className="mt-1 shrink-0 cursor-grab active:cursor-grabbing text-fg-faint hover:text-fg-subtle"
                              title={t("dragToSort")}
                            >
                              <GripVertical className="w-5 h-5" />
                            </div>
                            <div className="flex-1">
                              <input
                                type="text"
                                value={node.name}
                                onChange={(e) =>
                                  updateNode(node.id, "name", e.target.value)
                                }
                                className="bg-transparent text-fg font-semibold text-lg focus:outline-none border-b border-dashed border-line-strong focus:border-primary px-1 w-2/3"
                                placeholder={t("stepNamePlaceholder")}
                              />
                              <div className="mt-2 flex items-center gap-3">
                                <span
                                  className={`text-[10px] px-2 py-0.5 rounded-full font-medium ${
                                    node.nodeType === "review"
                                      ? "bg-purple-500/10 text-purple-400 border border-purple-500/30"
                                      : node.nodeType === "manual"
                                        ? "bg-warning/10 text-warning-fg border border-warning/30"
                                        : "bg-fg-faint/10 text-fg-subtle border border-line-strong/30"
                                  }`}
                                >
                                  {nodeTypeLabels[node.nodeType] || t("nodeTypeStandard")}
                                </span>
                                <span className="text-[10px] text-fg-faint">
                                  {t("timeoutLabel", { minutes: node.timeout })}
                                </span>
                                <span className="text-[10px] text-fg-faint">
                                  {t("maxRejectLabel", { count: node.maxRejectCycles ?? 5 })}
                                </span>
                              </div>
                            </div>
                          </div>
                          <button
                            onClick={() => removeNode(node.id)}
                            className="p-1.5 text-fg-faint hover:text-danger-fg hover:bg-surface-3 rounded transition-colors-fast"
                          >
                            <Trash2 className="w-4 h-4" />
                          </button>
                        </div>

                        <div className="grid grid-cols-2 gap-5">
                          <div>
                            <label className="block text-xs font-semibold text-fg-faint mb-1.5">
                              {t("nodeTypeLabel")}
                            </label>
                            <select
                              value={node.nodeType}
                              onChange={(e) =>
                                updateNode(
                                  node.id,
                                  "nodeType",
                                  e.target.value as WorkflowTemplateNode["nodeType"]
                                )
                              }
                              className="w-full bg-bg border border-line-strong rounded-lg px-3 py-2.5 text-sm text-fg-muted focus:outline-none focus:border-primary"
                            >
                              <option value="standard">
                                {"\u2699"} {t("optionStandard")}
                              </option>
                              <option value="review">
                                {"\uD83D\uDD0D"} {t("optionReview")}
                              </option>
                              <option value="manual">
                                {"\uD83D\uDC68"} {t("optionManual")}
                              </option>
                            </select>
                          </div>

                          <div>
                            <label className="block text-xs font-semibold text-fg-faint mb-1.5">
                              {t("assigneeLabel")}
                            </label>
                            <select
                              value={node.assigneeType}
                              onChange={(e) => {
                                const next = e.target
                                  .value as WorkflowTemplateNode["assigneeType"];
                                // 原子更新：一次 setState 同时写入 assigneeType 和
                                // assigneeId，避免两次 updateNode 基于同一份 stale
                                // formData 互相覆盖（第二次会把第一次的 assigneeType
                                // 回退回旧值，导致 select 切不回去）。
                                updateField(
                                  "nodes",
                                  formData.nodes.map((n) =>
                                    n.id === node.id
                                      ? {
                                          ...n,
                                          assigneeType: next,
                                          assigneeId:
                                            next !== "specific_agent"
                                              ? ""
                                              : n.assigneeId,
                                        }
                                      : n
                                  )
                                );
                              }}
                              className="w-full bg-bg border border-line-strong rounded-lg px-3 py-2.5 text-sm text-fg-muted focus:outline-none focus:border-primary"
                            >
                              <option value="any_agent">
                                {"\uD83E\uDD16"} {t("optionAnyAgent")}
                              </option>
                              <option value="specific_agent">
                                {"\uD83C\uDFAF"} {t("optionSpecificAgent")}
                              </option>
                              <option value="human">
                                {"\uD83D\uDC68"} {t("optionHuman")}
                              </option>
                            </select>
                            {node.assigneeType === "specific_agent" && (
                              <select
                                value={node.assigneeId || ""}
                                onChange={(e) =>
                                  updateNode(
                                    node.id,
                                    "assigneeId",
                                    e.target.value
                                  )
                                }
                                className="w-full mt-2 bg-surface border border-primary/50 rounded text-sm text-primary-fg p-2 focus:outline-none"
                              >
                                <option value="" disabled>
                                  {t("selectAgentPlaceholder")}
                                </option>
                                {agents.map((a) => (
                                  <option key={a.id} value={a.id}>
                                    {a.name}
                                  </option>
                                ))}
                              </select>
                            )}
                          </div>

                          <div>
                            <label className="block text-xs font-semibold text-fg-faint mb-1.5">
                              {t("timeoutMinutesLabel")}
                            </label>
                            <input
                              type="number"
                              value={node.timeout}
                              min={0}
                              max={480}
                              onChange={(e) =>
                                updateNode(
                                  node.id,
                                  "timeout",
                                  parseInt(e.target.value) || 0
                                )
                              }
                              className="w-full bg-bg border border-line-strong rounded-lg px-3 py-2 text-sm text-fg focus:outline-none focus:border-primary"
                            />
                          </div>

                          <div>
                            <label className="block text-xs font-semibold text-fg-faint mb-1.5">
                              {t("maxRejectCyclesLabel")}
                            </label>
                            <input
                              type="number"
                              value={node.maxRejectCycles ?? 5}
                              min={1}
                              max={20}
                              onChange={(e) =>
                                updateNode(
                                  node.id,
                                  "maxRejectCycles",
                                  parseInt(e.target.value) || 5
                                )
                              }
                              className="w-full bg-bg border border-line-strong rounded-lg px-3 py-2 text-sm text-fg focus:outline-none focus:border-primary"
                            />
                            <p className="text-[10px] text-fg-faint mt-1">
                              {t("maxRejectHint")}
                            </p>
                          </div>

                          <div className="col-span-2">
                            <label className="block text-xs font-semibold text-fg-faint mb-1.5">
                              {t("stepDescLabel")}
                            </label>
                            <textarea
                              value={node.description}
                              onChange={(e) =>
                                updateNode(
                                  node.id,
                                  "description",
                                  e.target.value
                                )
                              }
                              rows={2}
                              className="w-full bg-bg border border-line-strong rounded-lg px-3 py-2 text-sm text-fg-muted focus:outline-none focus:border-primary resize-none"
                              placeholder={t("stepDescPlaceholder")}
                            />
                          </div>

                          <div>
                            <label className="block text-xs font-semibold text-fg-faint mb-1.5 flex items-center">
                              <FolderGit2 className="w-3.5 h-3.5 mr-1 text-warning-fg" />{" "}
                              {t("readonlyDirsLabel")}
                            </label>
                            <input
                              type="text"
                              value={node.readonlyDirs || ""}
                              onChange={(e) =>
                                updateNode(
                                  node.id,
                                  "readonlyDirs",
                                  e.target.value
                                )
                              }
                              className="w-full bg-bg border border-warning/30 rounded-lg px-3 py-2 text-sm text-fg font-mono focus:outline-none focus:border-warning"
                              placeholder={t("readonlyDirsPlaceholder")}
                            />
                            <p className="text-[10px] text-fg-faint mt-1">
                              {t("readonlyDirsHint")}
                            </p>
                          </div>

                          <div>
                            <label className="block text-xs font-semibold text-fg-faint mb-1.5 flex items-center">
                              <FileOutput className="w-3.5 h-3.5 mr-1 text-success" />{" "}
                              {t("fullControlDirsLabel")}
                            </label>
                            <input
                              type="text"
                              value={node.fullControlDirs || ""}
                              onChange={(e) =>
                                updateNode(
                                  node.id,
                                  "fullControlDirs",
                                  e.target.value
                                )
                              }
                              className="w-full bg-bg border border-success/30 rounded-lg px-3 py-2 text-sm text-fg font-mono focus:outline-none focus:border-success"
                              placeholder={t("fullControlDirsPlaceholder")}
                            />
                            <p className="text-[10px] text-fg-faint mt-1">
                              {t("fullControlDirsHint")}
                            </p>
                          </div>

                          <div className="col-span-2">
                            <label className="block text-xs font-semibold text-fg-faint mb-1.5 flex items-center">
                              <FileOutput className="w-3.5 h-3.5 mr-1 text-accent" />{" "}
                              {t("artifactLabel")}
                            </label>
                            <input
                              type="text"
                              value={node.artifact || ""}
                              onChange={(e) =>
                                updateNode(
                                  node.id,
                                  "artifact",
                                  e.target.value
                                )
                              }
                              className="w-full bg-bg border border-line-strong rounded-lg px-3 py-2 text-sm text-fg focus:outline-none focus:border-success"
                            />
                          </div>
                        </div>
                      </div>
                    )}
                  </div>
                </div>
              );
            })}

            {/* 添加新节点 - 流程自然延续 */}
            <div
              className="relative z-10 flex items-start group"
              onDragOver={(e) => e.preventDefault()}
              onDrop={(e) => {
                e.preventDefault();
                handleDrop(e, formData.nodes.length);
              }}
            >
              <div className="flex flex-col items-center mr-6 mt-4">
                {formData.nodes.length > 1 && (
                  <ArrowDown className="w-5 h-5 text-fg-faint my-3" />
                )}
                <button
                  onClick={addNode}
                  className="w-14 h-14 rounded-full border-2 border-dashed border-line-strong hover:border-primary-fg bg-surface/50 hover:bg-primary/10 flex items-center justify-center text-fg-faint hover:text-primary-fg transition-all duration-200 group-hover:scale-105"
                >
                  <Plus className="w-6 h-6" />
                </button>
              </div>
              <div className="flex items-center h-14 pt-4">
                <span className="text-sm text-fg-faint group-hover:text-primary-fg transition-colors-fast">
                  {t("addStep")}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 运行模板（发起任务）
// ==========================================
interface TemplateRunViewProps {
  template: MappedTemplate;
  onCancel: () => void;
  onTaskCreated: (
    template: MappedTemplate,
    title: string,
    desc: string,
    taskType: string,
    priority: string,
    selectedProject: string,
    dueDate: string,
    labelList: string[],
    constraints: string
  ) => Promise<boolean | void>;
  projects: Project[];
}

function TemplateRunView({
  template,
  onCancel,
  onTaskCreated,
  projects,
}: TemplateRunViewProps) {
  const t = useTranslations("workflows.run");
  const [title, setTitle] = useState("");
  const [desc, setDesc] = useState("");
  const [constraints, setConstraints] = useState("");
  const [taskType, setTaskType] = useState("story");
  const [priority, setPriority] = useState("high");
  const [selectedProject, setSelectedProject] = useState(
    projects?.[0]?.id || ""
  );
  const [dueDate, setDueDate] = useState("");
  const [labels, setLabels] = useState("");
  const [isStarting, setIsStarting] = useState(false);

  const handleStart = async () => {
    if (!selectedProject) {
      return;
    }
    setIsStarting(true);
    try {
      const labelList = labels
        .split(",")
        .map((l) => l.trim())
        .filter(Boolean);
      const ok = await onTaskCreated(
        template,
        title,
        desc,
        taskType,
        priority,
        selectedProject,
        dueDate,
        labelList,
        constraints
      );
      if (!ok) {
        setIsStarting(false);
      }
    } catch {
      setIsStarting(false);
    }
  };

  const totalNodes = template.nodes.length;
  const humanNodes = template.nodes.filter(
    (n) => n.assigneeType === "human"
  ).length;
  const reviewNodes = template.nodes.filter(
    (n) => n.nodeType === "review"
  ).length;

  return (
    <div className="page-enter flex flex-col h-full relative bg-bg">
      <div className="p-6 border-b border-line-strong/50 bg-surface/50 backdrop-blur-md z-10 flex justify-between items-center shrink-0">
        <div className="flex items-center">
          <button
            onClick={onCancel}
            className="p-2 mr-4 text-fg-subtle hover:text-fg hover:bg-surface-2 rounded-lg transition-colors-fast"
          >
            <ArrowRight className="w-5 h-5 rotate-180" />
          </button>
          <div>
            <h1 className="text-2xl font-bold text-fg mb-1 flex items-center">
              {t("title")}{" "}
              <span className="ml-3 text-sm text-accent-fg bg-accent/10 border border-accent/20 px-2 py-0.5 rounded-full font-normal">
                {t("basedOn", { name: template.name })}
              </span>
            </h1>
            <p className="text-xs text-fg-subtle">
              {t("subtitle")}
            </p>
          </div>
        </div>
        <button
          onClick={handleStart}
          disabled={isStarting || !title || !desc || !selectedProject}
          className={`flex items-center px-8 py-2.5 rounded-lg font-bold transition-colors-normal shadow-lg btn-press ${
            isStarting
              ? "bg-accent/50 text-fg cursor-wait"
              : "bg-accent-strong hover:bg-accent text-fg shadow-accent/20"
          } disabled:opacity-50 disabled:cursor-not-allowed`}
        >
          {isStarting ? (
            <Loader2 className="w-5 h-5 mr-2 animate-spin" />
          ) : (
            <Rocket className="w-5 h-5 mr-2" />
          )}
          {isStarting ? t("triggering") : t("triggerButton")}
        </button>
      </div>

      <div className="flex-1 overflow-y-auto p-8 flex justify-center">
        <div className="w-full max-w-5xl grid grid-cols-12 gap-8">
          {/* 左侧：表单 */}
          <div className="col-span-12 lg:col-span-7 space-y-6">
            <div className="bg-surface-2/40 border border-line-strong/60 rounded-xl p-6">
              <h3 className="text-lg font-bold text-fg flex items-center mb-6">
                <div className="w-8 h-8 rounded-full bg-accent-strong/50 border border-accent text-accent-fg flex items-center justify-center mr-3 text-sm">
                  1
                </div>
                {t("stepOneHeader", { title: t("stepOneTitle") })}
              </h3>

              <div className="space-y-5">
                <div>
                  <label className="block text-sm font-semibold text-fg-muted mb-2">
                    {t("titleLabel")} <span className="text-danger-fg">*</span>
                  </label>
                  <input
                    type="text"
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                    className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg text-lg focus:outline-none focus:border-accent"
                    placeholder={t("titlePlaceholder")}
                  />
                </div>

                <div className="grid grid-cols-3 gap-4">
                  <div>
                    <label className="block text-sm font-semibold text-fg-muted mb-2">
                      {t("projectLabel")} <span className="text-danger-fg">*</span>
                    </label>
                    <select
                      value={selectedProject}
                      onChange={(e) => setSelectedProject(e.target.value)}
                      className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-accent"
                    >
                      {!projects?.length && (
                        <option value="">
                          {t("noProjects")}
                        </option>
                      )}
                      {projects?.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.name}
                        </option>
                      ))}
                    </select>
                    {!projects?.length && (
                      <p className="text-[10px] text-warning-fg mt-1">
                        {t("goCreateProject")}
                      </p>
                    )}
                  </div>
                  <div>
                    <label className="block text-sm font-semibold text-fg-muted mb-2">
                      {t("taskTypeLabel")}
                    </label>
                    <select
                      value={taskType}
                      onChange={(e) => setTaskType(e.target.value)}
                      className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-accent"
                    >
                      <option value="story">
                        {"\uD83D\uDCD6"} {t("typeStory")}
                      </option>
                      <option value="bug">{"\uD83D\uDC1B"} {t("typeBug")}</option>
                      <option value="task">
                        {"\uD83D\uDCCB"} {t("typeTask")}
                      </option>
                    </select>
                  </div>
                  <div>
                    <label className="block text-sm font-semibold text-fg-muted mb-2">
                      {t("priorityLabel")}
                    </label>
                    <select
                      value={priority}
                      onChange={(e) => setPriority(e.target.value)}
                      className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-accent"
                    >
                      <option value="urgent">
                        {"\uD83D\uDD34"} {t("prioUrgent")}
                      </option>
                      <option value="high">
                        {"\uD83D\uDFE0"} {t("prioHigh")}
                      </option>
                      <option value="medium">
                        {"\uD83D\uDFE1"} {t("prioMedium")}
                      </option>
                      <option value="low">{"\uD83D\uDFE2"} {t("prioLow")}</option>
                    </select>
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm font-semibold text-fg-muted mb-2">
                      {t("dueDateLabel")}
                    </label>
                    <input
                      type="date"
                      value={dueDate}
                      onChange={(e) => setDueDate(e.target.value)}
                      className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg text-sm focus:outline-none focus:border-accent"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-semibold text-fg-muted mb-2">
                      {t("labelsLabel")}
                    </label>
                    <input
                      type="text"
                      value={labels}
                      onChange={(e) => setLabels(e.target.value)}
                      className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg text-sm focus:outline-none focus:border-accent"
                      placeholder={t("labelsPlaceholder")}
                    />
                    <p className="text-[10px] text-fg-faint mt-1">
                      {t("labelsHint")}
                    </p>
                  </div>
                </div>

                <div>
                  <label className="block text-sm font-semibold text-fg-muted mb-2">
                    {t("constraintsLabel")}
                  </label>
                  <textarea
                    value={constraints}
                    onChange={(e) => setConstraints(e.target.value)}
                    rows={3}
                    className="w-full bg-surface border border-warning/30 rounded-lg px-4 py-3 text-warning-fg text-sm focus:outline-none focus:border-warning resize-none font-mono"
                    placeholder={t("constraintsPlaceholder")}
                  />
                  <p className="text-[10px] text-fg-faint mt-1">
                    {t("constraintsHint")}
                  </p>
                </div>

                <div>
                  <label className="block text-sm font-semibold text-fg-muted mb-2">
                    {t("descRequiredLabel")}<span className="text-danger-fg">*</span>
                  </label>
                  <textarea
                    value={desc}
                    onChange={(e) => setDesc(e.target.value)}
                    rows={10}
                    className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg-muted text-sm focus:outline-none focus:border-accent resize-none font-mono"
                    placeholder={t("descPlaceholder")}
                  />
                </div>
              </div>
            </div>
          </div>

          {/* 右侧：预览执行链路*/}
          <div className="col-span-12 lg:col-span-5">
            <div className="bg-surface border border-line rounded-xl p-6 sticky top-0 space-y-4">
              <h4 className="text-sm font-semibold text-fg-subtle uppercase tracking-wider flex items-center">
                <Workflow className="w-4 h-4 mr-2" /> {t("executionChain")}
              </h4>

              <div className="flex gap-3 text-xs">
                <span className="text-fg-subtle">{t("nodesCount", { count: totalNodes })}</span>
                <span className="text-fg-faint">·</span>
                <span className="text-warning-fg">{t("humanNodes", { count: humanNodes })}</span>
                <span className="text-fg-faint">·</span>
                <span className="text-purple-400">
                  {t("reviewNodes", { count: reviewNodes })}
                </span>
              </div>

              <div className="space-y-0 relative pl-4">
                <div className="absolute left-[27px] top-4 bottom-4 w-[2px] bg-surface-2 z-0" />

                {template.nodes.map((node, index) => {
                  const isFirst = index === 0;
                  const nodeTypeIcon =
                    node.nodeType === "review"
                      ? "\uD83D\uDD0D"
                      : node.nodeType === "manual"
                        ? "\uD83D\uDC68"
                        : "\u2699";
                  const assigneeLabel =
                    node.assigneeType === "human"
                      ? t("assigneeHuman")
                      : node.assigneeType === "specific_agent"
                        ? t("assigneeSpecific")
                        : t("assigneeAny");

                  return (
                    <div
                      key={node.id}
                      className="relative z-10 flex items-start mb-6 last:mb-0 group"
                    >
                      <div
                        className={`w-6 h-6 rounded-full border-2 flex items-center justify-center mr-4 shrink-0 mt-0.5 transition-colors-fast ${
                          isFirst
                            ? "bg-accent border-accent-fg node-glow"
                            : "bg-surface border-line-strong"
                        }`}
                      >
                        {isFirst && (
                          <div className="w-2 h-2 bg-fg rounded-full" />
                        )}
                      </div>
                      <div className="flex-1 min-w-0">
                        <div
                          className={`font-semibold mb-1 ${isFirst ? "text-accent-fg" : "text-fg-muted"}`}
                        >
                          {nodeTypeIcon} {node.name}
                        </div>
                        <div className="text-xs text-fg-faint mb-1 flex items-center gap-2">
                          <span>{assigneeLabel}</span>
                          {node.nodeType === "review" && (
                            <span className="text-purple-400">{t("reviewNodeTag")}</span>
                          )}
                        </div>
                        {node.description && (
                          <div className="text-[11px] text-fg-faint italic line-clamp-1">
                            {node.description}
                          </div>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 节点图预览弹窗
// ==========================================
interface NodeGraphPreviewProps {
  template: MappedTemplate;
  onClose: () => void;
}

function NodeGraphPreview({ template, onClose }: NodeGraphPreviewProps) {
  const t = useTranslations("workflows.preview");
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[700px] max-h-[80vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-line flex justify-between items-center sticky top-0 bg-surface z-10">
          <h2 className="text-lg font-bold text-fg flex items-center">
            <Workflow className="w-5 h-5 mr-2 text-primary-fg" />{" "}
            {template.name}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-surface-3 rounded-lg"
          >
            <X className="w-5 h-5 text-fg-subtle" />
          </button>
        </div>
        <div className="p-8 flex flex-col items-center">
          <div className="flex flex-col items-center w-full max-w-md">
            {template.nodes.map((node, idx) => {
              const nodeColors: Record<string, string> = {
                standard: "border-line-strong bg-surface-2",
                review: "border-purple-500 bg-purple-900/20",
                manual: "border-warning bg-warning-strong/20",
              };
              const c = nodeColors[node.nodeType] || nodeColors.standard;
              return (
                <React.Fragment key={node.id}>
                  <div
                    className={`w-full p-4 rounded-xl border-2 ${c} text-center animate-scale-in`}
                  >
                    <div className="text-xs text-fg-faint mb-1">
                      {node.nodeType === "review"
                        ? `\uD83D\uDD0D ${t("nodeReview")}`
                        : node.nodeType === "manual"
                          ? `\uD83D\uDC68 ${t("nodeManual")}`
                          : `\u2699 ${t("nodeStandard")}`}
                    </div>
                    <div className="text-fg font-bold text-lg">
                      {node.name}
                    </div>
                    <div className="text-xs text-fg-subtle mt-1">
                      {node.assigneeType === "human"
                        ? `\uD83D\uDC68 ${t("assigneeHumanShort")}`
                        : node.assigneeType === "specific_agent"
                          ? `\uD83C\uDFAF ${t("assigneeSpecificShort")}`
                          : `\uD83E\uDD16 ${t("assigneeAnyShort")}`}
                      {node.timeout > 0 && ` · ${node.timeout}min`}
                    </div>
                  </div>
                  {idx < template.nodes.length - 1 && (
                    <div className="flex flex-col items-center my-2">
                      <ArrowDown className="w-5 h-5 text-fg-faint" />
                    </div>
                  )}
                </React.Fragment>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 模板使用统计弹窗
// ==========================================
interface TemplateStatsDialogProps {
  template: MappedTemplate;
  onClose: () => void;
}

function TemplateStatsDialog({
  template,
  onClose,
}: TemplateStatsDialogProps) {
  const t = useTranslations("workflows.stats");
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[680px] max-h-[80vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-line flex justify-between items-center sticky top-0 bg-surface z-10">
          <h2 className="text-lg font-bold text-fg flex items-center">
            <BarChart3 className="w-5 h-5 mr-2 text-primary-fg" />{" "}
            {t("title")}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-surface-3 rounded-lg"
          >
            <X className="w-5 h-5 text-fg-subtle" />
          </button>
        </div>
        <div className="p-6 space-y-6">
          <div className="flex items-center gap-4">
            <div className="w-12 h-12 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-center">
              <Workflow className="w-6 h-6 text-primary-fg" />
            </div>
            <div>
              <h3 className="text-xl font-bold text-fg">
                {template.name}
              </h3>
              <p className="text-xs text-fg-subtle">
                {template.description?.substring(0, 60)}
              </p>
            </div>
          </div>

          <div className="grid grid-cols-3 gap-4">
            <div className="bg-surface-2/40 border border-line-strong/60 rounded-xl p-4 text-center">
              <div className="text-xs text-fg-faint mb-1">{t("refCount")}</div>
              <div className="text-2xl font-bold text-fg">-</div>
              <div className="text-[10px] text-fg-faint mt-1">{t("tasksUnit")}</div>
            </div>
            <div className="bg-surface-2/40 border border-line-strong/60 rounded-xl p-4 text-center">
              <div className="text-xs text-fg-faint mb-1">{t("avgCompletion")}</div>
              <div className="text-2xl font-bold text-fg flex items-center justify-center">
                <Clock className="w-5 h-5 mr-1 text-primary-fg" /> -
              </div>
              <div className="text-[10px] text-fg-faint mt-1">{t("endToEnd")}</div>
            </div>
            <div className="bg-surface-2/40 border border-line-strong/60 rounded-xl p-4 text-center">
              <div className="text-xs text-fg-faint mb-1">{t("nodeCountLabel")}</div>
              <div className="text-2xl font-bold text-fg">
                {template.nodes.length}
              </div>
              <div className="text-[10px] text-fg-faint mt-1">{t("stepsUnit")}</div>
            </div>
          </div>

          <div className="bg-surface-2/40 border border-line-strong/60 rounded-xl p-6 text-center">
            <BarChart3 className="w-8 h-8 text-fg-faint mx-auto mb-3" />
            <p className="text-fg-subtle text-sm">{t("noStats")}</p>
            <p className="text-fg-faint text-xs mt-1">
              {t("noStatsHint")}
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 模板列表
// ==========================================
interface TemplateListViewProps {
  templates: MappedTemplate[];
  onCreate: () => void;
  onEdit: (template: MappedTemplate) => void;
  onRun: (template: MappedTemplate) => void;
  onDelete: (templateId: string) => void;
}

function TemplateListView({
  templates,
  onCreate,
  onEdit,
  onRun,
  onDelete,
}: TemplateListViewProps) {
  const t = useTranslations("workflows.list");
  const { canEdit } = usePermission();
  const builtIn = templates.filter((t) => t.isBuiltIn);
  const custom = templates.filter((t) => !t.isBuiltIn);
  const [previewTemplate, setPreviewTemplate] =
    useState<MappedTemplate | null>(null);
  const [statsTemplate, setStatsTemplate] = useState<MappedTemplate | null>(
    null
  );

  const TemplateCard = ({ template }: { template: MappedTemplate }) => (
    <div
      className={`card-hover bg-surface-2/40 border border-line-strong/60 rounded-xl p-6 flex flex-col transition-all hover:bg-surface-2/50 shadow-sm backdrop-blur-sm hover:shadow-lg hover:shadow-primary/5 ${template.isBuiltIn ? "" : "hover:border-primary/30"}`}
    >
      <div className="flex justify-between items-start mb-3">
        <h3 className="text-lg font-bold text-fg flex items-center">
          <Workflow
            className={`w-5 h-5 mr-2 ${template.isBuiltIn ? "text-fg-subtle" : "text-primary-fg"}`}
          />
          {template.name}
        </h3>
        {template.isBuiltIn && (
          <span className="px-2 py-0.5 bg-surface-3 text-fg-muted text-xs rounded border border-line-strong">
            {t("builtInTag")}
          </span>
        )}
      </div>
      <p className="text-sm text-fg-subtle mb-4 flex-1 line-clamp-2">
        {template.description}
      </p>

      <div className="flex flex-wrap gap-2 mb-5">
        <span className="text-[10px] px-2 py-0.5 bg-surface-3/50 text-fg-subtle rounded">
          {t("nodeCount", { count: template.nodes.length })}
        </span>
        {template.nodes.filter((n) => n.nodeType === "review").length > 0 && (
          <span className="text-[10px] px-2 py-0.5 bg-purple-500/10 text-purple-400 rounded border border-purple-500/20">
            {t("reviewCount", { count: template.nodes.filter((n) => n.nodeType === "review").length })}
          </span>
        )}
        {template.nodes.filter((n) => n.nodeType === "manual").length > 0 && (
          <span className="text-[10px] px-2 py-0.5 bg-warning/10 text-warning-fg rounded border border-warning/20">
            {t("manualCount", { count: template.nodes.filter((n) => n.nodeType === "manual").length })}
          </span>
        )}
      </div>

      <div className="flex items-center text-xs border-t border-line-strong/50 pt-4 mt-auto">
        <div className="flex items-center space-x-3 ml-auto">
          <button
            onClick={() => setPreviewTemplate(template)}
            className="flex items-center text-fg-subtle hover:text-fg-muted transition-colors-fast"
          >
            <Eye className="w-3.5 h-3.5 mr-1" /> {t("preview")}
          </button>
          <button
            onClick={() => setStatsTemplate(template)}
            className="flex items-center text-fg-subtle hover:text-success-fg transition-colors-fast"
          >
            <BarChart3 className="w-3.5 h-3.5 mr-1" /> {t("stats")}
          </button>
          {!template.isBuiltIn && (
            <>
              <button
                onClick={() => onEdit(template)}
                disabled={!canEdit}
                className="flex items-center text-fg-subtle hover:text-primary-fg font-medium transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:text-fg-subtle"
              >
                <Edit2 className="w-3.5 h-3.5 mr-1" /> {t("edit")}
              </button>
              <button
                onClick={() => onDelete(template.id)}
                disabled={!canEdit}
                className="flex items-center text-fg-subtle hover:text-danger-fg transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:text-fg-subtle"
              >
                <Trash2 className="w-3.5 h-3.5 mr-1" /> {t("delete")}
              </button>
            </>
          )}
          <button
            onClick={() => onRun(template)}
            className="flex items-center px-3 py-1.5 bg-accent-strong/10 hover:bg-accent-strong/20 text-accent-fg border border-accent/30 rounded-lg font-medium transition-colors-fast btn-press"
          >
            <Rocket className="w-3.5 h-3.5 mr-1.5" /> {t("run")}
          </button>
        </div>
      </div>
    </div>
  );

  return (
    <div className="page-enter h-full flex flex-col p-8 overflow-y-auto">
      <div className="flex justify-between items-center mb-10 shrink-0">
        <div>
          <h1 className="text-3xl font-bold text-fg mb-1">
            {t("page.title")}
          </h1>
          <p className="text-sm text-fg-subtle">
            {t("page.subtitle", {
              total: templates.length,
              builtIn: builtIn.length,
            })}
          </p>
        </div>
        <button
          onClick={onCreate}
          disabled={!canEdit}
          className="flex items-center px-5 py-2.5 bg-primary-strong hover:bg-primary text-fg rounded-lg font-medium transition-colors-normal shadow-lg shadow-primary/20 btn-press disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <Plus className="w-5 h-5 mr-2" /> {t("page.createButton")}
        </button>
      </div>

      {/* 自定义模板*/}
      <div className="mb-10">
        <h2 className="text-sm font-semibold text-fg-faint uppercase tracking-wider mb-4 flex items-center">
          <LayoutTemplate className="w-4 h-4 mr-2" /> {t("customTemplates")}
        </h2>
        {custom.length > 0 ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {custom.map((t) => (
              <TemplateCard key={t.id} template={t} />
            ))}
          </div>
        ) : (
          <div className="bg-surface/50 border border-dashed border-line-strong rounded-xl p-10 text-center">
            <LayoutTemplate className="w-12 h-12 text-fg-faint mx-auto mb-4" />
            <p className="text-fg-subtle mb-4">
              {t("noCustomTemplates")}
            </p>
            <button
              onClick={onCreate}
              className="text-primary-fg hover:text-primary-fg font-medium text-sm"
            >
              {t("createFirst")}
            </button>
          </div>
        )}
      </div>

      {/* 内置模板 */}
      <div>
        <h2 className="text-sm font-semibold text-fg-faint uppercase tracking-wider mb-4 flex items-center">
          <Database className="w-4 h-4 mr-2" /> {t("builtInTemplates")}
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {builtIn.map((t) => (
            <TemplateCard key={t.id} template={t} />
          ))}
        </div>
      </div>

      {previewTemplate && (
        <ModalPortal>
        <NodeGraphPreview
          template={previewTemplate}
          onClose={() => setPreviewTemplate(null)}
        />
        </ModalPortal>
      )}
      {statsTemplate && (
        <ModalPortal>
        <TemplateStatsDialog
          template={statsTemplate}
          onClose={() => setStatsTemplate(null)}
        />
        </ModalPortal>
      )}
    </div>
  );
}

// ==========================================
// 工作流模板管理主视图 (Next.js Page)
// ==========================================
export default function WorkflowsPage() {
  const router = useRouter();
  const agents = useAppStore((s) => s.agents);
  const projects = useAppStore((s) => s.projects);
  const templates = useAppStore((s) => s.templates);
  const setTemplates = useAppStore((s) => s.setTemplates);
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);
  const loadProjectData = useAppStore((s) => s.loadProjectData);

  const [subView, setSubView] = useState<"list" | "editor" | "run">("list");
  const [editingTemplate, setEditingTemplate] =
    useState<MappedTemplate | null>(null);
  const t = useTranslations("workflows.page");
  const [runTemplate, setRunTemplate] = useState<MappedTemplate | null>(null);
  const [saving, setSaving] = useState(false);

  const wsId = currentWorkspaceId;

  const handleCreateNew = () => {
    const newTemplate: MappedTemplate = {
      id: "",
      name: t("newTemplateName"),
      description: t("newTemplateDesc"),
      isBuiltIn: false,
      triggerType: "manual",
      triggerConfig: {},
      triggerEnabled: true,
      nodes: [
        {
          id: Date.now(),
          name: t("defaultNodeName"),
          nodeType: "standard",
          assigneeType: "any_agent",
          assigneeId: "",
          description: "",
          timeout: 60,
          maxRejectCycles: 5,
          readonlyDirs: "/docs,/README.md",
          fullControlDirs: "/src",
          artifact: "",
        },
      ],
    };
    setEditingTemplate(newTemplate);
    setSubView("editor");
  };

  const handleEdit = (template: MappedTemplate) => {
    if (template.isBuiltIn) return;
    setEditingTemplate(JSON.parse(JSON.stringify(template)));
    setSubView("editor");
  };

  const handleRun = (template: MappedTemplate) => {
    setRunTemplate(JSON.parse(JSON.stringify(template)));
    setSubView("run");
  };

  const handleSave = async (saved: MappedTemplate) => {
    setSaving(true);
    try {
      const isNew = !saved.id;
      // Map frontend node format to API format
      const apiNodes = saved.nodes.map((n, idx) => ({
        name: n.name,
        node_type: n.nodeType,
        assignee_type: n.assigneeType,
        assignee_id: n.assigneeId || null,
        description: n.description || "",
        sort_order: idx,
        timeout_minutes: n.timeout || 0,
        readonly_dirs: n.readonlyDirs || "",
        full_control_dirs: n.fullControlDirs || "",
        artifact: n.artifact || "",
        max_reject_cycles: n.maxRejectCycles || 5,
      }));

      const triggerConfig = {
        project_id: saved.triggerConfig.projectId || "",
        interval_minutes: saved.triggerConfig.intervalMinutes || 0,
        title: saved.triggerConfig.title || "",
        description: saved.triggerConfig.description || "",
        repo_owner: saved.triggerConfig.repoOwner || "",
        repo_name: saved.triggerConfig.repoName || "",
        secret: saved.triggerConfig.secret || "",
      };

      const payload = {
        name: saved.name,
        description: saved.description,
        trigger_type: saved.triggerType,
        trigger_config: triggerConfig,
        trigger_enabled: saved.triggerEnabled,
        next_run_at: saved.nextRunAt || null,
        nodes: apiNodes,
      };

      if (wsId) {
        if (isNew) {
          const result = await api.createWorkflow(wsId, payload);
          const mapped = mapTemplateFromApi(result);
          setTemplates([...templates, mapped]);
        } else {
          const result = await api.updateWorkflow(wsId, saved.id, payload);
          const mapped = mapTemplateFromApi(result);
          setTemplates(templates.map((t) => (t.id === saved.id ? mapped : t)));
        }
      } else {
        // No workspace ID available — fall back to local state only
        const exists = templates.find((t) => t.id === saved.id);
        if (exists) {
          setTemplates(templates.map((t) => (t.id === saved.id ? saved : t)));
        } else {
          setTemplates([...templates, saved]);
        }
      }
      setSubView("list");
    } catch (err) {
      console.error("Save template failed:", err);
      // Still update local state on API failure so user doesn't lose work
      const exists = templates.find((t) => t.id === saved.id);
      if (exists) {
        setTemplates(templates.map((t) => (t.id === saved.id ? saved : t)));
      } else {
        setTemplates([...templates, saved]);
      }
      setSubView("list");
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (templateId: string) => {
    try {
      if (wsId) {
        await api.deleteWorkflow(wsId, templateId);
      }
      setTemplates(templates.filter((t) => t.id !== templateId));
    } catch (err) {
      console.error("Delete template failed:", err);
      // Still update local state on API failure
      setTemplates(templates.filter((t) => t.id !== templateId));
    }
  };

  const handleTaskCreated = async (
    template: MappedTemplate,
    title: string,
    desc: string,
    taskType: string,
    priority: string,
    selectedProject: string,
    dueDate: string,
    labelList: string[],
    constraints: string
  ): Promise<boolean> => {
    try {
      const payload = {
        title,
        description: desc,
        type: taskType,
        priority,
        due_date: dueDate,
        labels: labelList,
        constraints,
        workflow_template_id: template.id,
      };
      await api.createTask(selectedProject, payload);
      await loadProjectData(selectedProject);
      router.push("/dashboard");
      return true;
    } catch (err) {
      console.error("Create task failed:", err);
      return false;
    }
  };

  if (subView === "editor" && editingTemplate) {
    return (
      <TemplateEditorView
        template={editingTemplate}
        onSave={handleSave}
        onCancel={() => setSubView("list")}
        agents={agents}
        projects={projects}
        saving={saving}
      />
    );
  }
  if (subView === "run" && runTemplate) {
    return (
      <TemplateRunView
        template={runTemplate}
        onCancel={() => setSubView("list")}
        onTaskCreated={handleTaskCreated}
        projects={projects}
      />
    );
  }
  return (
    <TemplateListView
      templates={templates}
      onCreate={handleCreateNew}
      onEdit={handleEdit}
      onRun={handleRun}
      onDelete={handleDelete}
    />
  );
}
