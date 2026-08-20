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
  Star,
  GripVertical,
} from "lucide-react";
import { useAppStore } from "@/lib/store";
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
      name: "AI 加工处理",
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
    standard: "普通节点",
    review: "审查节点",
    manual: "人工操作",
  };

  return (
    <div className="page-enter flex flex-col min-h-[calc(100vh-3.5rem)] relative bg-slate-950">
      <div className="p-6 border-b border-slate-700/50 bg-slate-900/50 backdrop-blur-md z-10 flex justify-between items-center shrink-0 sticky top-0">
        <div className="flex items-center">
          <button
            onClick={onCancel}
            className="p-2 mr-4 text-slate-400 hover:text-white hover:bg-slate-800 rounded-lg transition-colors-fast"
          >
            <ArrowRight className="w-5 h-5 rotate-180" />
          </button>
          <div>
            <h1 className="text-2xl font-bold text-white mb-1">
              设计工作流程模板
            </h1>
            <p className="text-xs text-slate-400">
              模板定义了加工作业的协同与流转规则，可在不同的工程中重复使用。
            </p>
          </div>
        </div>
        <button
          onClick={() => onSave(formData)}
          disabled={saving}
          className={`flex items-center px-6 py-2.5 rounded-lg font-medium shadow-lg transition-colors-normal shrink-0 btn-press ${
            saving
              ? "bg-blue-500/50 text-white cursor-wait"
              : "bg-blue-600 hover:bg-blue-500 text-white shadow-blue-500/20"
          }`}
        >
          {saving ? (
            <Loader2 className="w-4 h-4 mr-2 animate-spin" />
          ) : (
            <Save className="w-4 h-4 mr-2" />
          )}
          {saving ? "保存中..." : "保存至模板库"}
        </button>
      </div>

      <div className="px-8 py-12 flex justify-center pb-24">
        <div className="w-full max-w-4xl">
          {/* 基础信息 */}
          <div className="mb-10 bg-slate-800/40 border border-slate-700/60 rounded-xl p-6">
            <div className="mb-4">
              <label className="block text-sm font-medium text-slate-400 mb-2">
                模板名称
              </label>
              <input
                type="text"
                value={formData.name}
                onChange={(e) => updateField("name", e.target.value)}
                className="w-full bg-slate-900 border border-slate-700 rounded-lg px-4 py-2.5 text-white font-semibold text-lg focus:outline-none focus:border-blue-500"
              />
            </div>
            <div className="grid grid-cols-2 gap-5 mb-4">
              <div>
                <label className="block text-sm font-medium text-slate-400 mb-2">
                  模板描述
                </label>
                <textarea
                  value={formData.description}
                  onChange={(e) => updateField("description", e.target.value)}
                  rows={2}
                  className="w-full bg-slate-900 border border-slate-700 rounded-lg px-4 py-2.5 text-slate-300 text-sm focus:outline-none focus:border-blue-500 resize-none"
                />
              </div>
            </div>
          </div>

          <div className="mb-10 bg-slate-800/40 border border-slate-700/60 rounded-xl p-6">
            <div className="flex items-center justify-between mb-5">
              <h3 className="text-lg font-bold text-white">触发方式</h3>
              <label className="flex items-center gap-2 text-sm text-slate-300">
                <input
                  type="checkbox"
                  checked={formData.triggerEnabled}
                  onChange={(e) => updateField("triggerEnabled", e.target.checked)}
                  className="h-4 w-4 rounded border-slate-600 bg-slate-950 text-blue-600 focus:ring-blue-500"
                />
                启用
              </label>
            </div>
            <div className="grid grid-cols-3 gap-3 mb-5">
              {[
                { value: "manual", label: "人工输入", icon: User },
                { value: "schedule", label: "定时触发", icon: Clock },
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
                        ? "border-blue-500 bg-blue-500/10 text-blue-200"
                        : "border-slate-700 bg-slate-950 text-slate-400 hover:border-slate-500"
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
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-slate-300 focus:outline-none focus:border-blue-500"
                >
                  <option value="">请选择项目</option>
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
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white focus:outline-none focus:border-blue-500"
                  placeholder="间隔分钟"
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
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white focus:outline-none focus:border-blue-500"
                />
                <input
                  type="text"
                  value={formData.triggerConfig.title || ""}
                  onChange={(e) => updateTriggerConfig("title", e.target.value)}
                  className="bg-slate-950 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white focus:outline-none focus:border-blue-500"
                  placeholder="任务标题"
                />
                <input
                  type="text"
                  value={formData.triggerConfig.description || ""}
                  onChange={(e) => updateTriggerConfig("description", e.target.value)}
                  className="bg-slate-950 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white focus:outline-none focus:border-blue-500"
                  placeholder="任务描述"
                />
              </div>
            )}

            {formData.triggerType === "github_issue" && (
              <div className="grid grid-cols-2 gap-4">
                <select
                  value={formData.triggerConfig.projectId || ""}
                  onChange={(e) => updateTriggerConfig("projectId", e.target.value)}
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-slate-300 focus:outline-none focus:border-blue-500"
                >
                  <option value="">请选择项目</option>
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
                  className="bg-slate-950 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white focus:outline-none focus:border-blue-500"
                  placeholder="仓库 Owner"
                />
                <input
                  type="text"
                  value={formData.triggerConfig.repoName || ""}
                  onChange={(e) => updateTriggerConfig("repoName", e.target.value)}
                  className="bg-slate-950 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white focus:outline-none focus:border-blue-500"
                  placeholder="仓库名称"
                />
                <input
                  type="password"
                  value={formData.triggerConfig.secret || ""}
                  onChange={(e) => updateTriggerConfig("secret", e.target.value)}
                  className="bg-slate-950 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white focus:outline-none focus:border-blue-500"
                  placeholder="Webhook Secret"
                />
              </div>
            )}
          </div>

          <h3 className="text-lg font-bold text-white mb-6 border-b border-slate-800 pb-2">
            工作流节点编排
          </h3>

          <div className="relative pl-2">
            <div className="absolute left-[35px] top-6 bottom-0 w-0.5 bg-slate-800 z-0" />

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
                            ? "bg-blue-900/30 border-blue-400 text-blue-400"
                            : "bg-slate-900 border-slate-700 text-slate-400 group-hover:border-blue-500/50"
                      }`}
                    >
                      {index + 1}
                    </div>
                    {index < formData.nodes.length - 1 && (
                      <ArrowDown className="w-5 h-5 text-slate-600 my-3" />
                    )}
                  </div>

                  <div
                    className={`flex-1 rounded-xl p-6 mb-6 transition-all bg-slate-800/40 border hover:shadow-lg ${
                      isDragOver
                          ? "border-blue-500/50 shadow-blue-500/10"
                          : "border-slate-700/60 hover:border-slate-500"
                    } ${isDragging ? "border-dashed border-slate-500" : ""}`}
                  >
                    {false ? (
                      <div>
                        <div className="flex items-center mb-4">
                          <h3 className="text-xl font-bold text-indigo-100 flex items-center">
                            <User className="w-5 h-5 mr-2 text-indigo-400" />{" "}
                            需求输入 (锁定)
                          </h3>
                        </div>
                        <p className="text-sm text-slate-400">
                          第一个节点固定为人工输入需求。
                        </p>
                      </div>
                    ) : (
                      <div>
                        <div className="flex justify-between items-start mb-5">
                          <div className="flex-1 flex items-start gap-2">
                            <div
                              className="mt-1 shrink-0 cursor-grab active:cursor-grabbing text-slate-600 hover:text-slate-400"
                              title="拖拽排序"
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
                                className="bg-transparent text-white font-semibold text-lg focus:outline-none border-b border-dashed border-slate-600 focus:border-blue-500 px-1 w-2/3"
                                placeholder="输入加工步骤名称..."
                              />
                              <div className="mt-2 flex items-center gap-3">
                                <span
                                  className={`text-[10px] px-2 py-0.5 rounded-full font-medium ${
                                    node.nodeType === "review"
                                      ? "bg-purple-500/10 text-purple-400 border border-purple-500/30"
                                      : node.nodeType === "manual"
                                        ? "bg-amber-500/10 text-amber-400 border border-amber-500/30"
                                        : "bg-slate-500/10 text-slate-400 border border-slate-500/30"
                                  }`}
                                >
                                  {nodeTypeLabels[node.nodeType] || "普通节点"}
                                </span>
                                <span className="text-[10px] text-slate-500">
                                  超时: {node.timeout}min
                                </span>
                                <span className="text-[10px] text-slate-500">
                                  驳回上限: {node.maxRejectCycles ?? 5}
                                </span>
                              </div>
                            </div>
                          </div>
                          <button
                            onClick={() => removeNode(node.id)}
                            className="p-1.5 text-slate-500 hover:text-red-400 hover:bg-slate-700 rounded transition-colors-fast"
                          >
                            <Trash2 className="w-4 h-4" />
                          </button>
                        </div>

                        <div className="grid grid-cols-2 gap-5">
                          <div>
                            <label className="block text-xs font-semibold text-slate-500 mb-1.5">
                              节点类型
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
                              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-slate-300 focus:outline-none focus:border-blue-500"
                            >
                              <option value="standard">
                                {"\u2699"} 普通节点 (Standard)
                              </option>
                              <option value="review">
                                {"\uD83D\uDD0D"} 审查节点 (Review)
                              </option>
                              <option value="manual">
                                {"\uD83D\uDC68"} 人工操作 (Manual)
                              </option>
                            </select>
                          </div>

                          <div>
                            <label className="block text-xs font-semibold text-slate-500 mb-1.5">
                              指派规则
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
                              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-slate-300 focus:outline-none focus:border-blue-500"
                            >
                              <option value="any_agent">
                                {"\uD83E\uDD16"} 任意空闲 AI 代理
                              </option>
                              <option value="specific_agent">
                                {"\uD83C\uDFAF"} 绑定特定 AI 代理
                              </option>
                              <option value="human">
                                {"\uD83D\uDC68"} 人类成员
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
                                className="w-full mt-2 bg-slate-900 border border-blue-500/50 rounded text-sm text-blue-300 p-2 focus:outline-none"
                              >
                                <option value="" disabled>
                                  -- 选择 Agent --
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
                            <label className="block text-xs font-semibold text-slate-500 mb-1.5">
                              超时时间 (分钟)
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
                              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
                            />
                          </div>

                          <div>
                            <label className="block text-xs font-semibold text-slate-500 mb-1.5">
                              最大驳回次数
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
                              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
                            />
                            <p className="text-[10px] text-slate-500 mt-1">
                              驳回达到阈值后自动进入人工干预
                            </p>
                          </div>

                          <div className="col-span-2">
                            <label className="block text-xs font-semibold text-slate-500 mb-1.5">
                              步骤描述 (Markdown)
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
                              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2 text-sm text-slate-300 focus:outline-none focus:border-blue-500 resize-none"
                              placeholder="描述该节点需要做什么..."
                            />
                          </div>

                          <div>
                            <label className="block text-xs font-semibold text-slate-500 mb-1.5 flex items-center">
                              <FolderGit2 className="w-3.5 h-3.5 mr-1 text-amber-400" />{" "}
                              只读目录
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
                              className="w-full bg-slate-950 border border-amber-500/30 rounded-lg px-3 py-2 text-sm text-white font-mono focus:outline-none focus:border-amber-500"
                              placeholder="如 /docs,/README.md（逗号分隔）"
                            />
                            <p className="text-[10px] text-slate-500 mt-1">
                              AI 代理可以读取但不能修改的目录/文件
                            </p>
                          </div>

                          <div>
                            <label className="block text-xs font-semibold text-slate-500 mb-1.5 flex items-center">
                              <FileOutput className="w-3.5 h-3.5 mr-1 text-emerald-500" />{" "}
                              完全控制目录
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
                              className="w-full bg-slate-950 border border-emerald-500/30 rounded-lg px-3 py-2 text-sm text-white font-mono focus:outline-none focus:border-emerald-500"
                              placeholder="如 /src（逗号分隔）"
                            />
                            <p className="text-[10px] text-slate-500 mt-1">
                              AI 代理可以自由读写代码的目录
                            </p>
                          </div>

                          <div className="col-span-2">
                            <label className="block text-xs font-semibold text-slate-500 mb-1.5 flex items-center">
                              <FileOutput className="w-3.5 h-3.5 mr-1 text-indigo-500" />{" "}
                              产物约束
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
                              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-emerald-500"
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
                  <ArrowDown className="w-5 h-5 text-slate-600 my-3" />
                )}
                <button
                  onClick={addNode}
                  className="w-14 h-14 rounded-full border-2 border-dashed border-slate-600 hover:border-blue-400 bg-slate-900/50 hover:bg-blue-500/10 flex items-center justify-center text-slate-500 hover:text-blue-400 transition-all duration-200 group-hover:scale-105"
                >
                  <Plus className="w-6 h-6" />
                </button>
              </div>
              <div className="flex items-center h-14 pt-4">
                <span className="text-sm text-slate-500 group-hover:text-blue-400 transition-colors-fast">
                  添加加工步骤
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
    <div className="page-enter flex flex-col h-full relative bg-slate-950">
      <div className="p-6 border-b border-slate-700/50 bg-slate-900/50 backdrop-blur-md z-10 flex justify-between items-center shrink-0">
        <div className="flex items-center">
          <button
            onClick={onCancel}
            className="p-2 mr-4 text-slate-400 hover:text-white hover:bg-slate-800 rounded-lg transition-colors-fast"
          >
            <ArrowRight className="w-5 h-5 rotate-180" />
          </button>
          <div>
            <h1 className="text-2xl font-bold text-white mb-1 flex items-center">
              发起工作流任务{" "}
              <span className="ml-3 text-sm text-indigo-400 bg-indigo-500/10 border border-indigo-500/20 px-2 py-0.5 rounded-full font-normal">
                基于: {template.name}
              </span>
            </h1>
            <p className="text-xs text-slate-400">
              填写需求信息后触发执行引擎，系统将自动驱动 AI 接管后续节点。
            </p>
          </div>
        </div>
        <button
          onClick={handleStart}
          disabled={isStarting || !title || !desc || !selectedProject}
          className={`flex items-center px-8 py-2.5 rounded-lg font-bold transition-colors-normal shadow-lg btn-press ${
            isStarting
              ? "bg-indigo-500/50 text-white cursor-wait"
              : "bg-indigo-600 hover:bg-indigo-500 text-white shadow-indigo-500/20"
          } disabled:opacity-50 disabled:cursor-not-allowed`}
        >
          {isStarting ? (
            <Loader2 className="w-5 h-5 mr-2 animate-spin" />
          ) : (
            <Rocket className="w-5 h-5 mr-2" />
          )}
          {isStarting ? "正在触发执行引擎..." : "触发执行引擎"}
        </button>
      </div>

      <div className="flex-1 overflow-y-auto p-8 flex justify-center">
        <div className="w-full max-w-5xl grid grid-cols-12 gap-8">
          {/* 左侧：表单 */}
          <div className="col-span-12 lg:col-span-7 space-y-6">
            <div className="bg-slate-800/40 border border-slate-700/60 rounded-xl p-6">
              <h3 className="text-lg font-bold text-white flex items-center mb-6">
                <div className="w-8 h-8 rounded-full bg-indigo-900/50 border border-indigo-500 text-indigo-400 flex items-center justify-center mr-3 text-sm">
                  1
                </div>
                完成首个节点：需求输入
              </h3>

              <div className="space-y-5">
                <div>
                  <label className="block text-sm font-semibold text-slate-300 mb-2">
                    任务标题 <span className="text-red-400">*</span>
                  </label>
                  <input
                    type="text"
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                    className="w-full bg-slate-900 border border-slate-700 rounded-lg px-4 py-3 text-white text-lg focus:outline-none focus:border-indigo-500"
                    placeholder="如 实现用户注册页面的前后端逻辑"
                  />
                </div>

                <div className="grid grid-cols-3 gap-4">
                  <div>
                    <label className="block text-sm font-semibold text-slate-300 mb-2">
                      目标工程 <span className="text-red-400">*</span>
                    </label>
                    <select
                      value={selectedProject}
                      onChange={(e) => setSelectedProject(e.target.value)}
                      className="w-full bg-slate-900 border border-slate-700 rounded-lg px-4 py-3 text-white focus:outline-none focus:border-indigo-500"
                    >
                      {!projects?.length && (
                        <option value="">
                          暂无工程，请先在工程设置中创建
                        </option>
                      )}
                      {projects?.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.name}
                        </option>
                      ))}
                    </select>
                    {!projects?.length && (
                      <p className="text-[10px] text-amber-400 mt-1">
                        请先前往「工程设置」页创建工程
                      </p>
                    )}
                  </div>
                  <div>
                    <label className="block text-sm font-semibold text-slate-300 mb-2">
                      任务类型
                    </label>
                    <select
                      value={taskType}
                      onChange={(e) => setTaskType(e.target.value)}
                      className="w-full bg-slate-900 border border-slate-700 rounded-lg px-4 py-3 text-white focus:outline-none focus:border-indigo-500"
                    >
                      <option value="story">
                        {"\uD83D\uDCD6"} Story (用户故事)
                      </option>
                      <option value="bug">{"\uD83D\uDC1B"} Bug (缺陷)</option>
                      <option value="task">
                        {"\uD83D\uDCCB"} Task (通用任务)
                      </option>
                    </select>
                  </div>
                  <div>
                    <label className="block text-sm font-semibold text-slate-300 mb-2">
                      优先级
                    </label>
                    <select
                      value={priority}
                      onChange={(e) => setPriority(e.target.value)}
                      className="w-full bg-slate-900 border border-slate-700 rounded-lg px-4 py-3 text-white focus:outline-none focus:border-indigo-500"
                    >
                      <option value="urgent">
                        {"\uD83D\uDD34"} 紧急 (Urgent)
                      </option>
                      <option value="high">
                        {"\uD83D\uDFE0"} 高 (High)
                      </option>
                      <option value="medium">
                        {"\uD83D\uDFE1"} 中 (Medium)
                      </option>
                      <option value="low">{"\uD83D\uDFE2"} 低 (Low)</option>
                    </select>
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm font-semibold text-slate-300 mb-2">
                      截止日期
                    </label>
                    <input
                      type="date"
                      value={dueDate}
                      onChange={(e) => setDueDate(e.target.value)}
                      className="w-full bg-slate-900 border border-slate-700 rounded-lg px-4 py-3 text-white text-sm focus:outline-none focus:border-indigo-500"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-semibold text-slate-300 mb-2">
                      标签（逗号分隔）：
                    </label>
                    <input
                      type="text"
                      value={labels}
                      onChange={(e) => setLabels(e.target.value)}
                      className="w-full bg-slate-900 border border-slate-700 rounded-lg px-4 py-3 text-white text-sm focus:outline-none focus:border-indigo-500"
                      placeholder="如 前端, 认证, OAuth2"
                    />
                    <p className="text-[10px] text-slate-500 mt-1">
                      最多 10 个标签
                    </p>
                  </div>
                </div>

                <div>
                  <label className="block text-sm font-semibold text-slate-300 mb-2">
                    约束与警告（红线要求）：
                  </label>
                  <textarea
                    value={constraints}
                    onChange={(e) => setConstraints(e.target.value)}
                    rows={3}
                    className="w-full bg-slate-900 border border-amber-500/30 rounded-lg px-4 py-3 text-amber-200 text-sm focus:outline-none focus:border-amber-500 resize-none font-mono"
                    placeholder="如 必须支持 OAuth2 标准协议；Token 必须加密存储..."
                  />
                  <p className="text-[10px] text-slate-500 mt-1">
                    这些约束将作为红线要求注入到 AI 代理的 system prompt 中
                  </p>
                </div>

                <div>
                  <label className="block text-sm font-semibold text-slate-300 mb-2">
                    详细需求说明<span className="text-red-400">*</span>
                  </label>
                  <textarea
                    value={desc}
                    onChange={(e) => setDesc(e.target.value)}
                    rows={10}
                    className="w-full bg-slate-900 border border-slate-700 rounded-lg px-4 py-3 text-slate-300 text-sm focus:outline-none focus:border-indigo-500 resize-none font-mono"
                    placeholder="在此输入详细的需求上下文。AI 代理将基于这份文档进行理解、架构设计和编码落地..."
                  />
                </div>
              </div>
            </div>
          </div>

          {/* 右侧：预览执行链路*/}
          <div className="col-span-12 lg:col-span-5">
            <div className="bg-slate-900 border border-slate-800 rounded-xl p-6 sticky top-0 space-y-4">
              <h4 className="text-sm font-semibold text-slate-400 uppercase tracking-wider flex items-center">
                <Workflow className="w-4 h-4 mr-2" /> 即将驱动的执行链路
              </h4>

              <div className="flex gap-3 text-xs">
                <span className="text-slate-400">{totalNodes} 节点</span>
                <span className="text-slate-600">·</span>
                <span className="text-amber-400">{humanNodes} 处需人工</span>
                <span className="text-slate-600">·</span>
                <span className="text-purple-400">
                  {reviewNodes} 处需审查
                </span>
              </div>

              <div className="space-y-0 relative pl-4">
                <div className="absolute left-[27px] top-4 bottom-4 w-[2px] bg-slate-800 z-0" />

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
                      ? "需人类操作"
                      : node.assigneeType === "specific_agent"
                        ? "指派特定 AI"
                        : "任意 AI 代理";

                  return (
                    <div
                      key={node.id}
                      className="relative z-10 flex items-start mb-6 last:mb-0 group"
                    >
                      <div
                        className={`w-6 h-6 rounded-full border-2 flex items-center justify-center mr-4 shrink-0 mt-0.5 transition-colors-fast ${
                          isFirst
                            ? "bg-indigo-500 border-indigo-400 node-glow"
                            : "bg-slate-900 border-slate-700"
                        }`}
                      >
                        {isFirst && (
                          <div className="w-2 h-2 bg-white rounded-full" />
                        )}
                      </div>
                      <div className="flex-1 min-w-0">
                        <div
                          className={`font-semibold mb-1 ${isFirst ? "text-indigo-400" : "text-slate-300"}`}
                        >
                          {nodeTypeIcon} {node.name}
                        </div>
                        <div className="text-xs text-slate-500 mb-1 flex items-center gap-2">
                          <span>{assigneeLabel}</span>
                          {node.nodeType === "review" && (
                            <span className="text-purple-400">审查节点</span>
                          )}
                        </div>
                        {node.description && (
                          <div className="text-[11px] text-slate-500 italic line-clamp-1">
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
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[700px] max-h-[80vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-800 flex justify-between items-center sticky top-0 bg-slate-900 z-10">
          <h2 className="text-lg font-bold text-white flex items-center">
            <Workflow className="w-5 h-5 mr-2 text-blue-400" />{" "}
            {template.name}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg"
          >
            <X className="w-5 h-5 text-slate-400" />
          </button>
        </div>
        <div className="p-8 flex flex-col items-center">
          <div className="flex flex-col items-center w-full max-w-md">
            {template.nodes.map((node, idx) => {
              const nodeColors: Record<string, string> = {
                standard: "border-slate-500 bg-slate-800",
                review: "border-purple-500 bg-purple-900/20",
                manual: "border-amber-500 bg-amber-900/20",
              };
              const c = nodeColors[node.nodeType] || nodeColors.standard;
              return (
                <React.Fragment key={node.id}>
                  <div
                    className={`w-full p-4 rounded-xl border-2 ${c} text-center animate-scale-in`}
                  >
                    <div className="text-xs text-slate-500 mb-1">
                      {node.nodeType === "review"
                        ? "\uD83D\uDD0D 审查节点"
                        : node.nodeType === "manual"
                          ? "\uD83D\uDC68 人工操作"
                          : "\u2699 普通节点"}
                    </div>
                    <div className="text-white font-bold text-lg">
                      {node.name}
                    </div>
                    <div className="text-xs text-slate-400 mt-1">
                      {node.assigneeType === "human"
                        ? "\uD83D\uDC68 人类"
                        : node.assigneeType === "specific_agent"
                          ? "\uD83C\uDFAF 指定代理"
                          : "\uD83E\uDD16 任意 AI"}
                      {node.timeout > 0 && ` · ${node.timeout}min`}
                    </div>
                  </div>
                  {idx < template.nodes.length - 1 && (
                    <div className="flex flex-col items-center my-2">
                      <ArrowDown className="w-5 h-5 text-slate-600" />
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
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[680px] max-h-[80vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-800 flex justify-between items-center sticky top-0 bg-slate-900 z-10">
          <h2 className="text-lg font-bold text-white flex items-center">
            <BarChart3 className="w-5 h-5 mr-2 text-blue-400" />{" "}
            模板使用统计
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg"
          >
            <X className="w-5 h-5 text-slate-400" />
          </button>
        </div>
        <div className="p-6 space-y-6">
          <div className="flex items-center gap-4">
            <div className="w-12 h-12 rounded-xl bg-blue-500/10 border border-blue-500/20 flex items-center justify-center">
              <Workflow className="w-6 h-6 text-blue-400" />
            </div>
            <div>
              <h3 className="text-xl font-bold text-white">
                {template.name}
              </h3>
              <p className="text-xs text-slate-400">
                {template.description?.substring(0, 60)}
              </p>
            </div>
          </div>

          <div className="grid grid-cols-3 gap-4">
            <div className="bg-slate-800/40 border border-slate-700/60 rounded-xl p-4 text-center">
              <div className="text-xs text-slate-500 mb-1">引用次数</div>
              <div className="text-2xl font-bold text-white">-</div>
              <div className="text-[10px] text-slate-500 mt-1">个任务</div>
            </div>
            <div className="bg-slate-800/40 border border-slate-700/60 rounded-xl p-4 text-center">
              <div className="text-xs text-slate-500 mb-1">平均完成时间</div>
              <div className="text-2xl font-bold text-white flex items-center justify-center">
                <Clock className="w-5 h-5 mr-1 text-blue-400" /> -
              </div>
              <div className="text-[10px] text-slate-500 mt-1">端到端</div>
            </div>
            <div className="bg-slate-800/40 border border-slate-700/60 rounded-xl p-4 text-center">
              <div className="text-xs text-slate-500 mb-1">节点数</div>
              <div className="text-2xl font-bold text-white">
                {template.nodes.length}
              </div>
              <div className="text-[10px] text-slate-500 mt-1">个步骤</div>
            </div>
          </div>

          <div className="bg-slate-800/40 border border-slate-700/60 rounded-xl p-6 text-center">
            <BarChart3 className="w-8 h-8 text-slate-600 mx-auto mb-3" />
            <p className="text-slate-400 text-sm">暂无统计数据</p>
            <p className="text-slate-500 text-xs mt-1">
              运行此模板的任务完成后，统计数据将自动生成
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
  defaultId: string;
  onSetDefault: (id: string) => void;
}

function TemplateListView({
  templates,
  onCreate,
  onEdit,
  onRun,
  onDelete,
  defaultId,
  onSetDefault,
}: TemplateListViewProps) {
  const builtIn = templates.filter((t) => t.isBuiltIn);
  const custom = templates.filter((t) => !t.isBuiltIn);
  const [previewTemplate, setPreviewTemplate] =
    useState<MappedTemplate | null>(null);
  const [statsTemplate, setStatsTemplate] = useState<MappedTemplate | null>(
    null
  );

  const TemplateCard = ({ template }: { template: MappedTemplate }) => (
    <div
      className={`card-hover bg-slate-800/40 border border-slate-700/60 rounded-xl p-6 flex flex-col transition-all hover:bg-slate-800/50 shadow-sm backdrop-blur-sm hover:shadow-lg hover:shadow-blue-500/5 ${template.isBuiltIn ? "" : "hover:border-blue-500/30"}`}
    >
      <div className="flex justify-between items-start mb-3">
        <h3 className="text-lg font-bold text-white flex items-center">
          <Workflow
            className={`w-5 h-5 mr-2 ${template.isBuiltIn ? "text-slate-400" : "text-blue-400"}`}
          />
          {template.name}
        </h3>
        {template.isBuiltIn && (
          <span className="px-2 py-0.5 bg-slate-700 text-slate-300 text-xs rounded border border-slate-600">
            系统内置
          </span>
        )}
      </div>
      <p className="text-sm text-slate-400 mb-4 flex-1 line-clamp-2">
        {template.description}
      </p>

      <div className="flex flex-wrap gap-2 mb-5">
        <span className="text-[10px] px-2 py-0.5 bg-slate-700/50 text-slate-400 rounded">
          {template.nodes.length} 个节点
        </span>
        {template.nodes.filter((n) => n.nodeType === "review").length > 0 && (
          <span className="text-[10px] px-2 py-0.5 bg-purple-500/10 text-purple-400 rounded border border-purple-500/20">
            审查{" "}
            {template.nodes.filter((n) => n.nodeType === "review").length}
          </span>
        )}
        {template.nodes.filter((n) => n.nodeType === "manual").length > 0 && (
          <span className="text-[10px] px-2 py-0.5 bg-amber-500/10 text-amber-400 rounded border border-amber-500/20">
            需人工{" "}
            {template.nodes.filter((n) => n.nodeType === "manual").length}
          </span>
        )}
      </div>

      <div className="flex items-center text-xs border-t border-slate-700/50 pt-4 mt-auto">
        <div className="flex items-center space-x-3 ml-auto">
          <button
            onClick={() => setPreviewTemplate(template)}
            className="flex items-center text-slate-400 hover:text-slate-300 transition-colors-fast"
          >
            <Eye className="w-3.5 h-3.5 mr-1" /> 预览
          </button>
          <button
            onClick={() => setStatsTemplate(template)}
            className="flex items-center text-slate-400 hover:text-emerald-400 transition-colors-fast"
          >
            <BarChart3 className="w-3.5 h-3.5 mr-1" /> 统计
          </button>
          {!template.isBuiltIn && (
            <>
              <button
                onClick={() => onEdit(template)}
                className="flex items-center text-slate-400 hover:text-blue-400 font-medium transition-colors-fast"
              >
                <Edit2 className="w-3.5 h-3.5 mr-1" /> 编辑
              </button>
              <button
                onClick={() => onDelete(template.id)}
                className="flex items-center text-slate-400 hover:text-red-400 transition-colors-fast"
              >
                <Trash2 className="w-3.5 h-3.5 mr-1" /> 删除
              </button>
            </>
          )}
          {defaultId === template.id ? (
            <span className="flex items-center px-3 py-1.5 bg-amber-500/10 text-amber-400 border border-amber-500/30 rounded-lg font-medium cursor-not-allowed">
              <Star className="w-3.5 h-3.5 mr-1.5" /> 默认
            </span>
          ) : (
            <button
              onClick={() => onSetDefault?.(template.id)}
              className="flex items-center px-3 py-1.5 bg-amber-500/5 hover:bg-amber-500/15 text-amber-400 border border-amber-500/20 rounded-lg font-medium transition-colors-fast"
            >
              <Star className="w-3.5 h-3.5 mr-1.5" /> 设为默认
            </button>
          )}
          <button
            onClick={() => onRun(template)}
            className="flex items-center px-3 py-1.5 bg-indigo-600/10 hover:bg-indigo-600/20 text-indigo-400 border border-indigo-500/30 rounded-lg font-medium transition-colors-fast btn-press"
          >
            <Rocket className="w-3.5 h-3.5 mr-1.5" /> 运行
          </button>
        </div>
      </div>
    </div>
  );

  return (
    <div className="page-enter h-full flex flex-col p-8 overflow-y-auto">
      <div className="flex justify-between items-center mb-10 shrink-0">
        <div>
          <h1 className="text-3xl font-bold text-white mb-1">
            工作流模板管理
          </h1>
          <p className="text-sm text-slate-400">
            管理工作区级的工作流标准。共 {templates.length} 个模板，
            {builtIn.length} 个内置可用。
          </p>
        </div>
        <button
          onClick={onCreate}
          className="flex items-center px-5 py-2.5 bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-medium transition-colors-normal shadow-lg shadow-blue-500/20 btn-press"
        >
          <Plus className="w-5 h-5 mr-2" /> 新建自定义模板
        </button>
      </div>

      {/* 自定义模板*/}
      <div className="mb-10">
        <h2 className="text-sm font-semibold text-slate-500 uppercase tracking-wider mb-4 flex items-center">
          <LayoutTemplate className="w-4 h-4 mr-2" /> 自定义模板
        </h2>
        {custom.length > 0 ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {custom.map((t) => (
              <TemplateCard key={t.id} template={t} />
            ))}
          </div>
        ) : (
          <div className="bg-slate-900/50 border border-dashed border-slate-700 rounded-xl p-10 text-center">
            <LayoutTemplate className="w-12 h-12 text-slate-600 mx-auto mb-4" />
            <p className="text-slate-400 mb-4">
              当前工作区暂时没有自定义模板。
            </p>
            <button
              onClick={onCreate}
              className="text-blue-400 hover:text-blue-300 font-medium text-sm"
            >
              点击创建第一个模板
            </button>
          </div>
        )}
      </div>

      {/* 内置模板 */}
      <div>
        <h2 className="text-sm font-semibold text-slate-500 uppercase tracking-wider mb-4 flex items-center">
          <Database className="w-4 h-4 mr-2" /> 系统内置模板
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {builtIn.map((t) => (
            <TemplateCard key={t.id} template={t} />
          ))}
        </div>
      </div>

      {previewTemplate && (
        <NodeGraphPreview
          template={previewTemplate}
          onClose={() => setPreviewTemplate(null)}
        />
      )}
      {statsTemplate && (
        <TemplateStatsDialog
          template={statsTemplate}
          onClose={() => setStatsTemplate(null)}
        />
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
  const [defaultTemplateId, setDefaultTemplateId] = useState("tpl_builtin_1");
  const [editingTemplate, setEditingTemplate] =
    useState<MappedTemplate | null>(null);
  const [runTemplate, setRunTemplate] = useState<MappedTemplate | null>(null);
  const [saving, setSaving] = useState(false);

  const wsId = currentWorkspaceId;

  const handleCreateNew = () => {
    const newTemplate: MappedTemplate = {
      id: "",
      name: "新建自定义模板",
      description: "在此输入模板描述...",
      isBuiltIn: false,
      triggerType: "manual",
      triggerConfig: {},
      triggerEnabled: true,
      nodes: [
        {
          id: Date.now(),
          name: "AI 加工处理",
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
      defaultId={defaultTemplateId}
      onSetDefault={setDefaultTemplateId}
    />
  );
}
