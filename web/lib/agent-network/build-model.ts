// build-model.ts: 从真实 Agent/任务数据构建 3D 协作网络的节点与边(纯函数)。
// 数据规则:
// 1. 一个真实 Agent 对应一个节点,不虚构演示实体;
// 2. 任务 nodeAgents 中相邻且不同的真实 Agent 生成一条交接边,同一对 Agent 的
//    多个任务合并到同一条边的 taskIds;
// 3. 边的 active 仅由处于 running/ready/waiting 的任务驱动;已结束任务保留在
//    taskIds 中但不产生流光;
// 4. 节点坐标由 Agent ID 排序后确定性计算,与传入顺序无关;缺失数据不会让节点
//    重叠或产生 NaN。
import type { MappedAgent, MappedTask } from "@/lib/types";
import type { AgentNetworkEdge, AgentNetworkModel, AgentNetworkNode } from "./types";

/** 产生流光的任务当前状态集合 */
const ACTIVE_TASK_STATES = new Set(["running", "ready", "waiting"]);

/** 确定性字符串哈希(FNV-1a),用作布局抖动种子 */
function hashSeed(text: string): number {
  let hash = 0x811c9dc5;
  for (let i = 0; i < text.length; i += 1) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0) / 0x100000000;
}

/** 黄金角螺旋布局:单节点居中,多节点环绕分布且带个体起伏 */
function layoutPosition(index: number, total: number, seed: number): [number, number, number] {
  if (total <= 1) return [0, 0, 0.6];
  const angle = index * Math.PI * (3 - Math.sqrt(5)) + (seed - 0.5) * 0.6;
  const ring = 2.9 * Math.max(1, Math.sqrt(total / 6));
  const radius = ring * (0.72 + seed * 0.36);
  return [Math.cos(angle) * radius, Math.sin(angle) * radius, (seed - 0.5) * 2.4];
}

export function buildAgentNetworkModel(
  agents: MappedAgent[],
  tasks: MappedTask[],
  projectId: string | null,
): AgentNetworkModel {
  const sortedAgents = [...agents].sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  const nodes = new Map<string, AgentNetworkNode>();
  sortedAgents.forEach((agent, index) => {
    if (!agent.id || nodes.has(agent.id)) return;
    nodes.set(agent.id, {
      id: agent.id,
      name: agent.name || agent.id,
      provider: agent.provider || "",
      status: agent.status,
      activeTaskIds: [],
      position: layoutPosition(index, sortedAgents.length, hashSeed(agent.id)),
    });
  });
  if (nodes.size === 0) return { nodes: [], edges: [] };

  // 任务按工程过滤:仅统计当前工程的交接关系
  const scopedTasks = projectId
    ? tasks.filter((task) => task._projectId === projectId)
    : [];

  // nodeAgents 里的值通常是 Agent 名称(mapTaskFromApi);看板降级行会放
  // assignee_id,所以 id 也纳入解析。name 缺失的 Agent 无法被任务引用。
  const nameToId = new Map<string, string>();
  for (const node of nodes.values()) {
    nameToId.set(node.id, node.id);
    if (node.name && !nameToId.has(node.name)) nameToId.set(node.name, node.id);
  }

  const edges = new Map<string, AgentNetworkEdge>();
  for (const task of scopedTasks) {
    const taskActive = ACTIVE_TASK_STATES.has(task.priorityState);
    const involved = (task.nodeAgents || [])
      .map((name) => nameToId.get(name))
      .filter((id): id is string => !!id);
    if (taskActive) {
      for (const id of new Set(involved)) {
        nodes.get(id)!.activeTaskIds.push(task.id);
      }
    }
    for (let i = 0; i + 1 < involved.length; i += 1) {
      const source = involved[i];
      const target = involved[i + 1];
      if (source === target) continue;
      const [lo, hi] = source < target ? [source, target] : [target, source];
      const key = `${lo}|${hi}`;
      let edge = edges.get(key);
      if (!edge) {
        edge = { id: key, source: lo, target: hi, taskIds: [], active: false };
        edges.set(key, edge);
      }
      if (!edge.taskIds.includes(task.id)) edge.taskIds.push(task.id);
      if (taskActive) edge.active = true;
    }
  }

  return { nodes: [...nodes.values()], edges: [...edges.values()] };
}
