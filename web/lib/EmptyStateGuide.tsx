"use client";
// 首次使用/空态引导卡片：插在主要数据页的空态位置，告诉新用户"这是干嘛的、怎么开始"，
// 并提供完整使用指南的外部链接。

import { Compass, ExternalLink, CheckCircle2 } from "lucide-react";

// 完整使用指南地址（首次登录教程文档）
export const GUIDE_URL = "http://zhenfeng.chat";

interface GuideDef {
  title: string;
  desc: string;
  steps: string[];
}

const GUIDES: Record<string, GuideDef> = {
  project: {
    title: "从这里开始：创建你的第一个工程",
    desc: "工程对应一个真实的 Git 代码仓库，AI 代理在其中开发。需要可用的仓库凭据，任务才能实际运转。",
    steps: [
      "在「工程设置」创建工程，填写名称并关联 Git 仓库地址",
      "在「AI 代理」注册代理，选择编码工具（如 AtomCode、Claude Code）",
      "在「工作流模板」选择或新建一个流程模板",
      "从模板「运行」发起任务，在「任务监控」跟踪执行与人工审批",
    ],
  },
  agents: {
    title: "还没有 AI 代理",
    desc: "代理是真正干活编码的机器人。注册代理后生成 API Token，再把配置写入守护进程 config.yaml 并启动，它就开始接单。",
    steps: [
      "点击「注册新代理」，填写名称、编码工具、Git 身份",
      "创建后复制一次性 API Token 与守护进程配置",
      "在代理机器上安装 CLI，把配置写入 ~/.teammate/config.yaml",
      "运行 teammate-agentd 启动守护进程，等待其连接服务并认领节点",
    ],
  },
  dashboard: {
    title: "还没有任务",
    desc: "任务监控台展示所有任务的执行进度。任务由「工作流模板」发起，AI 代理按节点逐步处理，审查/人工节点需你介入。",
    steps: [
      "选择一个「工作流模板」，点击「运行」",
      "填写需求标题与详细描述，选择目标工程",
      "触发执行引擎，AI 代理自动接管后续节点",
      "在「审查队列」处理 review 节点，或在此进行通过/退回/人工介入",
    ],
  },
  history: {
    title: "还没有历史任务",
    desc: "这里汇总已结束（完成/取消）的任务与执行记录，方便回顾各节点做了什么。运行任务后会自动出现在此处。",
    steps: [
      "在一个「工作流模板」上点击「运行」发起任务",
      "任务成功后返回本页查看历史执行详情",
    ],
  },
  reviews: {
    title: "待审查队列为空，很好",
    desc: "工作流中的「审查节点」到达后必须由人类批准，不会自转。有需要你审批的任务时会出现在这里。",
    steps: [
      "当任务推进到 review 节点时，其会进入本队列",
      "进入任务后点击「通过」放行，或「退回」要求返工",
    ],
  },
  memory: {
    title: "还没有共享记忆",
    desc: "共享记忆是训练好的知识沉淀，供所有代理跨任务复用。你可以在这里查看代理写入的经验与教训。",
    steps: [
      "代理正常执行任务后会沉淀记忆条目",
      "可用语义搜索检索过往经验，让后续任务少走弯路",
    ],
  },
  skills: {
    title: "配置技能与 MCP",
    desc: "技能为代理注入领域能力（如如何对接某个柜台），MCP 服务器让代理调用外部工具。两者都能提升代理的实际效果。",
    steps: [
      "配置需要的技能与 MCP 服务器",
      "回到「AI 代理」把技能/MCP 绑定到对应代理",
    ],
  },
};

export default function EmptyStateGuide({
  page,
}: {
  page: keyof typeof GUIDES;
}) {
  const guide = GUIDES[page];
  if (!guide) return null;

  return (
    <div className="bg-gradient-to-br from-slate-800/40 to-slate-900/40 border border-slate-700/60 rounded-xl p-6">
      <div className="flex items-start gap-3 mb-3">
        <div className="w-10 h-10 rounded-xl bg-blue-500/10 border border-blue-500/20 flex items-center justify-center shrink-0">
          <Compass className="w-5 h-5 text-blue-400" />
        </div>
        <div>
          <h3 className="text-base font-bold text-white">{guide.title}</h3>
          <p className="text-xs text-slate-400 mt-1 leading-relaxed">
            {guide.desc}
          </p>
        </div>
      </div>

      <ol className="space-y-2 my-4">
        {guide.steps.map((step, i) => (
          <li key={i} className="flex items-start gap-2 text-sm text-slate-300">
            <CheckCircle2 className="w-4 h-4 mt-0.5 text-emerald-400 shrink-0" />
            <span className="leading-relaxed">{step}</span>
          </li>
        ))}
      </ol>

      <a
        href={GUIDE_URL}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center text-xs font-medium text-blue-400 hover:text-blue-300 transition-colors-fast"
      >
        <ExternalLink className="w-3.5 h-3.5 mr-1.5" />
        查看完整操作指南
      </a>
    </div>
  );
}