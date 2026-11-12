"use client";
// AgentNetworkScene.tsx: 原生 Three.js 渲染的真实 Agent 协作网络场景。
// 只消费 AgentNetworkModel;颜色全部取自语义 CSS 变量并随 data-theme 切换更新;
// DPR 上限 1.5;页面不可见或 prefers-reduced-motion 时停用连续动画;
// 卸载时释放全部 geometry/material/texture/renderer 与事件监听。

import { useEffect, useMemo, useRef } from "react";
import * as THREE from "three";
import type { AgentStatus } from "@/lib/types";
import type { AgentNetworkModel } from "./types";
import { readThemeColors, withAlpha, type ThemeColorKey } from "./theme-colors";

interface AgentNetworkSceneProps {
  model: AgentNetworkModel;
  selectedAgentId: string | null;
  onSelectAgent: (agentId: string) => void;
  /** 状态文案由 i18n 层注入,保证 3D 标签与界面语言一致 */
  formatStatus: (status: AgentStatus) => string;
  /** 画布无障碍标签,由 i18n 层注入 */
  ariaLabel: string;
  /** WebGL 初始化失败时通知面板降级为二维列表 */
  onWebglError: () => void;
}

/** 状态 → 主题色键:节点/环/流光全部随主题取色,不写死色调 */
const STATUS_COLOR_KEY: Record<AgentStatus, ThemeColorKey> = {
  online: "success",
  busy: "accent",
  paused: "warning",
  pending: "warning",
  offline: "lineStrong",
};

interface NodeVisual {
  id: string;
  name: string;
  provider: string;
  status: AgentStatus;
  group: THREE.Group;
  material: THREE.MeshStandardMaterial;
  glow: THREE.SpriteMaterial;
  ring: THREE.Mesh<THREE.TorusGeometry, THREE.MeshBasicMaterial>;
  labelCanvas: HTMLCanvasElement;
  labelTexture: THREE.CanvasTexture;
  labelMaterial: THREE.SpriteMaterial;
  phase: number;
}

interface PulseVisual {
  mesh: THREE.Mesh<THREE.SphereGeometry, THREE.MeshBasicMaterial>;
  curve: THREE.CatmullRomCurve3;
  offset: number;
}

interface EdgeVisual {
  active: boolean;
  lineMaterial: THREE.LineBasicMaterial;
  pulses: PulseVisual[];
}

/** 白色径向光晕贴图,与主题无关,着色由材质 color 完成 */
function makeGlowTexture(): THREE.CanvasTexture {
  const canvas = document.createElement("canvas");
  canvas.width = 128;
  canvas.height = 128;
  const ctx = canvas.getContext("2d");
  if (!ctx) return new THREE.CanvasTexture(canvas);
  const gradient = ctx.createRadialGradient(64, 64, 2, 64, 64, 62);
  gradient.addColorStop(0, "rgba(255,255,255,0.95)");
  gradient.addColorStop(0.14, "rgba(255,255,255,0.5)");
  gradient.addColorStop(0.42, "rgba(255,255,255,0.12)");
  gradient.addColorStop(1, "rgba(255,255,255,0)");
  ctx.fillStyle = gradient;
  ctx.fillRect(0, 0, 128, 128);
  return new THREE.CanvasTexture(canvas);
}

export default function AgentNetworkScene({
  model,
  selectedAgentId,
  onSelectAgent,
  formatStatus,
  ariaLabel,
  onWebglError,
}: AgentNetworkSceneProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const modelRef = useRef(model);
  const selectedRef = useRef(selectedAgentId);
  const onSelectRef = useRef(onSelectAgent);
  const formatStatusRef = useRef(formatStatus);
  const onWebglErrorRef = useRef(onWebglError);
  const reducedRef = useRef(false);
  const rebuildRef = useRef<(() => void) | null>(null);
  const renderOnceRef = useRef<(() => void) | null>(null);

  modelRef.current = model;
  selectedRef.current = selectedAgentId;
  onSelectRef.current = onSelectAgent;
  formatStatusRef.current = formatStatus;
  onWebglErrorRef.current = onWebglError;

  // 结构签名:节点/边在视觉上有意义的字段变化才触发重建,
  // 周期性任务刷新产生的新对象但同内容不会打断动画
  const signature = useMemo(
    () =>
      JSON.stringify({
        n: model.nodes.map((n) => [n.id, n.name, n.provider, n.status, n.position]),
        e: model.edges.map((e) => [e.id, e.active]),
      }),
    [model]
  );

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true });
    } catch {
      onWebglErrorRef.current();
      return;
    }
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 1.5));
    renderer.outputColorSpace = THREE.SRGBColorSpace;

    let colors = readThemeColors();
    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(42, 1, 0.1, 100);
    camera.position.set(0, 0.5, 11.5);
    const network = new THREE.Group();
    scene.add(network);

    const ambient = new THREE.AmbientLight(0xffffff, 0.9);
    const keyLight = new THREE.PointLight(0xffffff, 24, 40);
    keyLight.position.set(4, 6, 9);
    scene.add(ambient, keyLight);

    // 背景星尘:确定性序列分布,轻微自转只作氛围,不参与数据表达
    const starCount = 420;
    const starPositions = new Float32Array(starCount * 3);
    for (let i = 0; i < starCount; i += 1) {
      const radius = 8 + ((i * 2.399) % 11);
      const theta = (i * 1.131) % (Math.PI * 2);
      const phi = Math.acos(2 * ((i * 0.617) % 1) - 1);
      starPositions[i * 3] = radius * Math.sin(phi) * Math.cos(theta);
      starPositions[i * 3 + 1] = radius * Math.sin(phi) * Math.sin(theta) * 0.6;
      starPositions[i * 3 + 2] = radius * Math.cos(phi) - 4;
    }
    const starGeometry = new THREE.BufferGeometry();
    starGeometry.setAttribute("position", new THREE.BufferAttribute(starPositions, 3));
    const starMaterial = new THREE.PointsMaterial({
      color: colors.lineStrong,
      size: 0.03,
      transparent: true,
      opacity: 0.4,
      depthWrite: false,
    });
    const stars = new THREE.Points(starGeometry, starMaterial);
    scene.add(stars);

    // 语义网格:网络下方的水平细网格,颜色随主题
    const gridExtent = 14;
    const gridStep = 1.4;
    const gridPoints: number[] = [];
    for (let v = -gridExtent; v <= gridExtent + 0.001; v += gridStep) {
      gridPoints.push(-gridExtent, 0, v, gridExtent, 0, v);
      gridPoints.push(v, 0, -gridExtent, v, 0, gridExtent);
    }
    const gridGeometry = new THREE.BufferGeometry();
    gridGeometry.setAttribute("position", new THREE.Float32BufferAttribute(gridPoints, 3));
    const gridMaterial = new THREE.LineBasicMaterial({
      color: colors.line,
      transparent: true,
      opacity: 0.5,
      depthWrite: false,
    });
    const grid = new THREE.LineSegments(gridGeometry, gridMaterial);
    grid.position.y = -3.4;
    scene.add(grid);

    // 共享几何体:节点尺寸一致,仅位置与材质不同
    const nodeGeometry = new THREE.IcosahedronGeometry(0.17, 1);
    const ringGeometry = new THREE.TorusGeometry(0.32, 0.01, 8, 48);
    const pulseGeometry = new THREE.SphereGeometry(0.045, 8, 8);
    const glowTexture = makeGlowTexture();

    const visuals: NodeVisual[] = [];
    const nodeById = new Map<string, NodeVisual>();
    const raycastTargets: THREE.Mesh[] = [];
    const edgeVisuals: EdgeVisual[] = [];
    const rebuildDisposables: { dispose(): void }[] = [];

    const statusColor = (status: AgentStatus) => colors[STATUS_COLOR_KEY[status]];

    const drawLabel = (visual: NodeVisual) => {
      const ctx = visual.labelCanvas.getContext("2d");
      if (!ctx) return;
      visual.labelCanvas.width = 512;
      visual.labelCanvas.height = 144;
      ctx.clearRect(0, 0, 512, 144);
      ctx.fillStyle = withAlpha(colors.surface, 0.86);
      ctx.strokeStyle = withAlpha(colors.lineStrong, 0.9);
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.roundRect(10, 10, 492, 124, 22);
      ctx.fill();
      ctx.stroke();
      ctx.fillStyle = statusColor(visual.status).getStyle();
      ctx.beginPath();
      ctx.arc(42, 48, 8, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = colors.fg.getStyle();
      ctx.font = "600 30px system-ui, sans-serif";
      const name = visual.name.length > 13 ? `${visual.name.slice(0, 12)}…` : visual.name;
      ctx.fillText(name, 62, 58);
      ctx.fillStyle = colors.fgSubtle.getStyle();
      ctx.font = "500 21px ui-monospace, monospace";
      const subText = [visual.provider.toUpperCase(), formatStatusRef.current(visual.status)]
        .filter(Boolean)
        .join(" · ");
      ctx.fillText(subText.slice(0, 34), 28, 106);
      visual.labelTexture.needsUpdate = true;
    };

    const clearNetwork = () => {
      network.clear();
      for (const item of rebuildDisposables) item.dispose();
      rebuildDisposables.length = 0;
      visuals.length = 0;
      nodeById.clear();
      raycastTargets.length = 0;
      edgeVisuals.length = 0;
    };

    const rebuild = () => {
      clearNetwork();
      const current = modelRef.current;
      for (const node of current.nodes) {
        const color = statusColor(node.status);
        const group = new THREE.Group();
        group.position.set(...node.position);
        group.userData.agentId = node.id;

        const material = new THREE.MeshStandardMaterial({
          color,
          emissive: color,
          emissiveIntensity: 1.4,
          roughness: 0.25,
          metalness: 0.45,
        });
        const mesh = new THREE.Mesh(nodeGeometry, material);
        mesh.userData.agentId = node.id;
        group.add(mesh);

        const glow = new THREE.SpriteMaterial({
          map: glowTexture,
          color,
          transparent: true,
          opacity: 0.5,
          blending: THREE.AdditiveBlending,
          depthWrite: false,
        });
        const glowSprite = new THREE.Sprite(glow);
        glowSprite.scale.setScalar(1.15);
        group.add(glowSprite);

        const ringMaterial = new THREE.MeshBasicMaterial({
          color,
          transparent: true,
          opacity: 0.3,
          blending: THREE.AdditiveBlending,
          depthWrite: false,
        });
        const ring = new THREE.Mesh(ringGeometry, ringMaterial);
        ring.rotation.x = Math.PI * 0.34;
        group.add(ring);

        const labelCanvas = document.createElement("canvas");
        const labelTexture = new THREE.CanvasTexture(labelCanvas);
        labelTexture.colorSpace = THREE.SRGBColorSpace;
        const labelMaterial = new THREE.SpriteMaterial({
          map: labelTexture,
          transparent: true,
          depthWrite: false,
          opacity: 0.95,
        });
        const label = new THREE.Sprite(labelMaterial);
        label.position.set(0, -0.8, 0);
        label.scale.set(2.3, 0.65, 1);
        group.add(label);

        network.add(group);
        const visual: NodeVisual = {
          id: node.id,
          name: node.name,
          provider: node.provider,
          status: node.status,
          group,
          material,
          glow,
          ring,
          labelCanvas,
          labelTexture,
          labelMaterial,
          phase: (node.position[0] + node.position[1]) * 1.7,
        };
        drawLabel(visual);
        visuals.push(visual);
        nodeById.set(node.id, visual);
        raycastTargets.push(mesh);
        rebuildDisposables.push(material, glow, ringMaterial, labelTexture, labelMaterial);
      }

      for (const edge of current.edges) {
        const startNode = nodeById.get(edge.source);
        const endNode = nodeById.get(edge.target);
        if (!startNode || !endNode) continue;
        const start = startNode.group.position.clone();
        const end = endNode.group.position.clone();
        const middle = start
          .clone()
          .lerp(end, 0.5)
          .add(new THREE.Vector3(0, 0.16 + start.distanceTo(end) * 0.09, 0.3));
        const curve = new THREE.CatmullRomCurve3([start, middle, end]);
        const lineGeometry = new THREE.BufferGeometry().setFromPoints(curve.getPoints(36));
        const lineMaterial = new THREE.LineBasicMaterial({
          color: edge.active ? colors.accent : colors.lineStrong,
          transparent: true,
          opacity: edge.active ? 0.6 : 0.3,
          blending: THREE.AdditiveBlending,
          depthWrite: false,
        });
        network.add(new THREE.Line(lineGeometry, lineMaterial));

        const pulses: PulseVisual[] = [];
        if (edge.active && !reducedRef.current) {
          for (let copy = 0; copy < 2; copy += 1) {
            const pulseMaterial = new THREE.MeshBasicMaterial({
              color: copy === 0 ? colors.accent : colors.primary,
              transparent: true,
              opacity: 0.9,
              blending: THREE.AdditiveBlending,
              depthWrite: false,
            });
            const pulse = new THREE.Mesh(pulseGeometry, pulseMaterial);
            network.add(pulse);
            pulses.push({ mesh: pulse, curve, offset: copy * 0.5 });
            rebuildDisposables.push(pulseMaterial);
          }
        }
        edgeVisuals.push({ active: edge.active, lineMaterial, pulses });
        rebuildDisposables.push(lineGeometry, lineMaterial);
      }

      if (reducedRef.current) renderer.render(scene, camera);
    };
    rebuildRef.current = rebuild;

    // reduced-motion 的单帧重绘:按当前选中态落定节点缩放后渲染一次
    renderOnceRef.current = () => {
      if (!reducedRef.current) return;
      const selected = selectedRef.current;
      for (const visual of visuals) {
        const target = visual.id === selected ? 1.18 : 1;
        visual.group.scale.setScalar(target);
      }
      renderer.render(scene, camera);
    };

    const applyTheme = () => {
      colors = readThemeColors();
      starMaterial.color = colors.lineStrong;
      gridMaterial.color = colors.line;
      for (const visual of visuals) {
        const color = statusColor(visual.status);
        visual.material.color = color;
        visual.material.emissive = color;
        visual.glow.color = color;
        visual.ring.material.color = color;
        drawLabel(visual);
      }
      for (const edge of edgeVisuals) {
        edge.lineMaterial.color = edge.active ? colors.accent : colors.lineStrong;
        edge.pulses.forEach((pulse, index) => {
          pulse.mesh.material.color = index === 0 ? colors.accent : colors.primary;
        });
      }
    };

    const themeObserver = new MutationObserver(() => {
      applyTheme();
      if (reducedRef.current) renderer.render(scene, camera);
    });
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });

    const reducedQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    reducedRef.current = reducedQuery.matches;

    const clock = new THREE.Clock();
    const tmpScale = new THREE.Vector3();
    const pointer = new THREE.Vector2();
    const pointerTarget = { x: 0, y: 0 };
    const raycaster = new THREE.Raycaster();
    let hovered = "";
    let frame = 0;

    const renderFrame = () => {
      const elapsed = clock.getElapsedTime();
      camera.position.x += (pointerTarget.x - camera.position.x) * 0.03;
      camera.position.y += (0.5 + pointerTarget.y - camera.position.y) * 0.03;
      camera.lookAt(0, 0, 0);
      // 网络只做有界摆动,不无限旋转,避免用户迷失方向
      network.rotation.y = Math.sin(elapsed * 0.13) * 0.07;
      network.rotation.x = Math.cos(elapsed * 0.1) * 0.02;
      stars.rotation.y = elapsed * 0.006;
      const selected = selectedRef.current;
      for (const visual of visuals) {
        const highlighted = visual.id === selected || visual.id === hovered;
        const target = highlighted ? 1.18 : 1 + Math.sin(elapsed * 2.1 + visual.phase) * 0.02;
        visual.group.scale.lerp(tmpScale.set(target, target, target), 0.1);
      }
      for (const edge of edgeVisuals) {
        for (const pulse of edge.pulses) {
          const progress = (elapsed * 0.09 + pulse.offset) % 1;
          pulse.mesh.position.copy(pulse.curve.getPointAt(progress));
          pulse.mesh.material.opacity = Math.sin(progress * Math.PI) * 0.95;
        }
      }
      renderer.render(scene, camera);
      frame = requestAnimationFrame(renderFrame);
    };

    const startLoop = () => {
      if (frame !== 0 || reducedRef.current || document.hidden) return;
      clock.getDelta();
      frame = requestAnimationFrame(renderFrame);
    };
    const stopLoop = () => {
      if (frame !== 0) {
        cancelAnimationFrame(frame);
        frame = 0;
      }
    };

    // 连续渲染的唯一裁决入口:同时满足"在视口内、页面可见、未启用 reduced-motion"
    // 才允许 rAF 运行,任一条件变化都从这里重新裁决,避免多套条件组合出漏洞
    let inView = true;
    const syncLoop = () => {
      if (inView && !document.hidden && !reducedRef.current) startLoop();
      else stopLoop();
    };

    const handleVisibility = () => syncLoop();
    document.addEventListener("visibilitychange", handleVisibility);

    // 面板滚出视口时暂停连续渲染,回到视口恢复
    const intersectionObserver = new IntersectionObserver((entries) => {
      inView = entries[0]?.isIntersecting ?? true;
      syncLoop();
    }, { threshold: 0.05 });
    intersectionObserver.observe(canvas);

    const handleReducedChange = () => {
      reducedRef.current = reducedQuery.matches;
      // 流光结构依赖该开关,切换时重建以增删流光
      rebuild();
      syncLoop();
    };
    reducedQuery.addEventListener("change", handleReducedChange);

    const updatePointer = (event: PointerEvent) => {
      const rect = canvas.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) return;
      pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
      pointerTarget.x = pointer.x * 0.45;
      pointerTarget.y = pointer.y * 0.28;
      raycaster.setFromCamera(pointer, camera);
      hovered = raycaster.intersectObjects(raycastTargets, false)[0]?.object.userData.agentId ?? "";
      canvas.style.cursor = hovered ? "pointer" : "default";
      if (reducedRef.current) renderer.render(scene, camera);
    };
    const handlePointerDown = (event: PointerEvent) => {
      // 触屏 tap 不触发 pointermove,这里用事件坐标补一次命中检测
      const rect = canvas.getBoundingClientRect();
      pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
      raycaster.setFromCamera(pointer, camera);
      hovered = raycaster.intersectObjects(raycastTargets, false)[0]?.object.userData.agentId ?? "";
      if (!hovered) return;
      onSelectRef.current(hovered);
      // reduced-motion 无连续动画,选中后直接落定缩放并单帧重绘给出反馈
      if (reducedRef.current) renderOnceRef.current?.();
    };
    const handlePointerLeave = () => {
      hovered = "";
      canvas.style.cursor = "default";
    };
    canvas.addEventListener("pointermove", updatePointer);
    canvas.addEventListener("pointerdown", handlePointerDown);
    canvas.addEventListener("pointerleave", handlePointerLeave);

    const resize = () => {
      const { clientWidth, clientHeight } = canvas;
      if (clientWidth === 0 || clientHeight === 0) return;
      renderer.setSize(clientWidth, clientHeight, false);
      camera.aspect = clientWidth / clientHeight;
      camera.updateProjectionMatrix();
      if (reducedRef.current) renderer.render(scene, camera);
    };
    const resizeObserver = new ResizeObserver(resize);
    resizeObserver.observe(canvas);
    resize();

    syncLoop();
    if (reducedRef.current) renderer.render(scene, camera);

    return () => {
      stopLoop();
      resizeObserver.disconnect();
      intersectionObserver.disconnect();
      themeObserver.disconnect();
      document.removeEventListener("visibilitychange", handleVisibility);
      reducedQuery.removeEventListener("change", handleReducedChange);
      canvas.removeEventListener("pointermove", updatePointer);
      canvas.removeEventListener("pointerdown", handlePointerDown);
      canvas.removeEventListener("pointerleave", handlePointerLeave);
      clearNetwork();
      nodeGeometry.dispose();
      ringGeometry.dispose();
      pulseGeometry.dispose();
      glowTexture.dispose();
      starGeometry.dispose();
      starMaterial.dispose();
      gridGeometry.dispose();
      gridMaterial.dispose();
      renderer.dispose();
      renderer.forceContextLoss();
      rebuildRef.current = null;
      renderOnceRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    rebuildRef.current?.();
  }, [signature]);

  // 选中态变化:reduced-motion 下单帧重绘给出高亮反馈
  useEffect(() => {
    renderOnceRef.current?.();
  }, [selectedAgentId]);

  return (
    <canvas
      ref={canvasRef}
      className="absolute inset-0 h-full w-full touch-none"
      role="img"
      aria-label={ariaLabel}
    />
  );
}
