"use client";
// 模板只读浏览页:以画布形态展示模板节点与依赖连线(内置/自定义模板均可浏览)。
// 数据来自模板列表接口(节点 uuid + depends_on 建边),dagre 布局,节点不可拖/不可连/无小地图。

import { Suspense, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { ArrowLeft } from "lucide-react";
import api from "@/lib/api";
import RunFlowCanvas, { type RunFlowNode } from "@/lib/workflow-canvas/RunFlowDemo";
import { useAppStore } from "@/lib/store";

function ViewInner() {
  const t = useTranslations("workflows.view");
  const router = useRouter();
  const searchParams = useSearchParams();
  const templateId = searchParams.get("templateId");
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const [nodes, setNodes] = useState<RunFlowNode[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    if (loaded || !templateId || !currentWorkspaceId) return;
    let cancelled = false;
    (async () => {
      try {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const list = (await api.listWorkflows(currentWorkspaceId)) as any[];
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const raw = list?.find((item: any) => (item.template?.id ?? item.id) === templateId);
        if (cancelled) return;
        if (!raw) throw new Error("not found");
        const tpl = raw.template ?? raw;
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const rawNodes: any[] = (raw.nodes ?? tpl.nodes ?? []).slice().sort(
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          (a: any, b: any) => (a.sort_order ?? 0) - (b.sort_order ?? 0)
        );
        setName(tpl.name || "");
        setDescription(tpl.description || "");
        const assigneeShort = (at: string) =>
          at === "human" ? t("assigneeHuman") : at === "specific_agent" ? t("assigneeSpecific") : t("assigneeAny");
        setNodes(
          rawNodes.map((n) => ({
            id: String(n.id),
            name: n.name || "",
            node_type: n.node_type || "standard",
            status: "pending",
            depends_on: (n.depends_on ?? []).map(String),
            assigneeShort: assigneeShort(n.assignee_type || "any_agent"),
            timeoutMinutes: n.timeout_minutes ?? 0,
          }))
        );
        setLoaded(true);
      } catch {
        if (!cancelled) {
          setError(t("loadFailed"));
          setLoaded(true);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [templateId, currentWorkspaceId, loaded]);

  return (
    <div className="h-full flex flex-col">
      {/* 顶栏 */}
      <div className="flex items-center gap-3 px-4 py-2.5 border-b border-line bg-surface shrink-0">
        <button
          onClick={() => router.push("/workflows")}
          className="flex items-center gap-1.5 text-xs text-fg-subtle hover:text-fg px-2 py-1.5 rounded-md hover:bg-surface-2"
        >
          <ArrowLeft className="w-3.5 h-3.5" /> {t("back")}
        </button>
        <div className="w-px h-5 bg-line" />
        <span className="text-sm font-semibold text-fg truncate">{name || t("loading")}</span>
        {description && <span className="text-xs text-fg-faint truncate flex-1">{description}</span>}
        {error && <span className="text-xs text-danger-fg bg-danger/10 px-2 py-1 rounded-md">{error}</span>}
      </div>

      {/* 只读画布 */}
      <div className="flex-1 min-h-0 p-4">
        {nodes.length > 0 ? (
          <RunFlowCanvas nodes={nodes} variant="template" />
        ) : (
          !error && <div className="h-full flex items-center justify-center text-xs text-fg-faint">{t("loading")}</div>
        )}
      </div>
    </div>
  );
}

export default function WorkflowViewPage() {
  return (
    <Suspense fallback={null}>
      <ViewInner />
    </Suspense>
  );
}
