// agent-network/types.ts: 3D 协作网络的输入模型。
// 节点与边全部来自真实 Agent 与任务数据,不含任何演示实体;
// 场景组件只消费该模型,不直接感知 store 类型。
import type { AgentStatus } from "@/lib/types";

export interface AgentNetworkNode {
  id: string;
  name: string;
  provider: string;
  status: AgentStatus;
  /** 该 Agent 当前处于活跃状态(running/ready/waiting)的任务 id */
  activeTaskIds: number[];
  /** 确定性布局坐标:由 Agent ID 决定,与传入顺序无关 */
  position: [number, number, number];
}

export interface AgentNetworkEdge {
  /** 端点为 Agent id;A→B 与 B→A 属同一对,合并为一条边 */
  id: string;
  source: string;
  target: string;
  /** 产生过该交接关系的任务 id(含已结束任务,用于点击查看) */
  taskIds: number[];
  /** 任一关联任务处于 running/ready/waiting 时为 true,驱动流光动画 */
  active: boolean;
}

export interface AgentNetworkModel {
  nodes: AgentNetworkNode[];
  edges: AgentNetworkEdge[];
}
