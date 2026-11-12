"use client";
// AmbientAgentNetwork.tsx: 登录页纯装饰的抽象粒子网络。
// 只含抽象粒子、少量连线与品牌中心点,不承载任何 Agent/任务含义,
// 因此不构成假数据;aria-hidden 且不接收指针事件。
// 颜色取语义变量并随 data-theme 更新;reduced-motion 时只渲染单帧;
// 卸载时释放全部 geometry/material/texture/renderer 与监听器。

import { useEffect, useRef } from "react";
import * as THREE from "three";
import { readThemeColors, type ThemeColors } from "./theme-colors";

export default function AmbientAgentNetwork() {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true });
    } catch {
      // 登录页装饰场景失败时静默退出,不影响登录流程
      return;
    }
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 1.5));
    renderer.outputColorSpace = THREE.SRGBColorSpace;

    let colors: ThemeColors = readThemeColors();
    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(45, 1, 0.1, 100);
    camera.position.set(0, 0.4, 9.5);
    camera.lookAt(0, 0, 0);

    const ambientGroup = new THREE.Group();
    scene.add(ambientGroup);

    // 抽象粒子:确定性序列分布在扁球壳上
    const count = 220;
    const positions = new Float32Array(count * 3);
    for (let i = 0; i < count; i += 1) {
      const radius = 3 + ((i * 1.713) % 4.5);
      const theta = (i * 2.055) % (Math.PI * 2);
      const phi = Math.acos(2 * ((i * 0.487) % 1) - 1);
      positions[i * 3] = radius * Math.sin(phi) * Math.cos(theta);
      positions[i * 3 + 1] = radius * Math.cos(phi) * 0.55;
      positions[i * 3 + 2] = radius * Math.sin(phi) * Math.sin(theta) * 0.8;
    }
    const pointsGeometry = new THREE.BufferGeometry();
    pointsGeometry.setAttribute("position", new THREE.BufferAttribute(positions, 3));
    const pointsMaterial = new THREE.PointsMaterial({
      color: colors.lineStrong,
      size: 0.045,
      transparent: true,
      opacity: 0.55,
      depthWrite: false,
    });
    const points = new THREE.Points(pointsGeometry, pointsMaterial);
    ambientGroup.add(points);

    // 品牌中心点:发光核心 + 两条语义色环
    const glowCanvas = document.createElement("canvas");
    glowCanvas.width = 128;
    glowCanvas.height = 128;
    const glowCtx = glowCanvas.getContext("2d");
    if (glowCtx) {
      const gradient = glowCtx.createRadialGradient(64, 64, 2, 64, 64, 62);
      gradient.addColorStop(0, "rgba(255,255,255,0.95)");
      gradient.addColorStop(0.14, "rgba(255,255,255,0.5)");
      gradient.addColorStop(0.42, "rgba(255,255,255,0.12)");
      gradient.addColorStop(1, "rgba(255,255,255,0)");
      glowCtx.fillStyle = gradient;
      glowCtx.fillRect(0, 0, 128, 128);
    }
    const glowTexture = new THREE.CanvasTexture(glowCanvas);
    const glowMaterial = new THREE.SpriteMaterial({
      map: glowTexture,
      color: colors.primary,
      transparent: true,
      opacity: 0.7,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
    });
    const core = new THREE.Sprite(glowMaterial);
    core.scale.setScalar(1.6);
    ambientGroup.add(core);

    const ringGeometry = new THREE.TorusGeometry(1.15, 0.012, 8, 72);
    const ringAMaterial = new THREE.MeshBasicMaterial({
      color: colors.primary,
      transparent: true,
      opacity: 0.28,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
    });
    const ringA = new THREE.Mesh(ringGeometry, ringAMaterial);
    ringA.rotation.x = Math.PI * 0.42;
    ambientGroup.add(ringA);
    const ringBMaterial = new THREE.MeshBasicMaterial({
      color: colors.accent,
      transparent: true,
      opacity: 0.2,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
    });
    const ringB = new THREE.Mesh(ringGeometry, ringBMaterial);
    ringB.rotation.set(Math.PI * 0.12, Math.PI * 0.3, 0);
    ringB.scale.setScalar(1.45);
    ambientGroup.add(ringB);

    // 少量连线:中心向外的细辐射线
    const spokeGeometry = new THREE.BufferGeometry();
    const spokePoints: number[] = [];
    for (let i = 0; i < 8; i += 1) {
      const theta = (i / 8) * Math.PI * 2;
      const phi = Math.acos(2 * ((i * 0.733) % 1) - 1);
      spokePoints.push(0, 0, 0);
      spokePoints.push(
        3.4 * Math.sin(phi) * Math.cos(theta),
        3.4 * Math.cos(phi) * 0.55,
        3.4 * Math.sin(phi) * Math.sin(theta) * 0.8
      );
    }
    spokeGeometry.setAttribute("position", new THREE.Float32BufferAttribute(spokePoints, 3));
    const spokeMaterial = new THREE.LineBasicMaterial({
      color: colors.lineStrong,
      transparent: true,
      opacity: 0.22,
      depthWrite: false,
    });
    const spokes = new THREE.LineSegments(spokeGeometry, spokeMaterial);
    ambientGroup.add(spokes);

    const reducedQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    let reduced = reducedQuery.matches;

    const clock = new THREE.Clock();
    let frame = 0;
    const renderFrame = () => {
      const elapsed = clock.getElapsedTime();
      ambientGroup.rotation.y = elapsed * 0.05;
      ringA.rotation.z = elapsed * 0.14;
      ringB.rotation.z = -elapsed * 0.09;
      renderer.render(scene, camera);
      frame = reduced ? 0 : requestAnimationFrame(renderFrame);
    };
    const startLoop = () => {
      if (frame !== 0 || reduced || document.hidden) return;
      clock.getDelta();
      frame = requestAnimationFrame(renderFrame);
    };
    const stopLoop = () => {
      if (frame !== 0) {
        cancelAnimationFrame(frame);
        frame = 0;
      }
    };

    const applyTheme = () => {
      colors = readThemeColors();
      pointsMaterial.color = colors.lineStrong;
      glowMaterial.color = colors.primary;
      ringAMaterial.color = colors.primary;
      ringBMaterial.color = colors.accent;
      spokeMaterial.color = colors.lineStrong;
    };

    const themeObserver = new MutationObserver(() => {
      applyTheme();
      if (reduced) renderer.render(scene, camera);
    });
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });

    const handleVisibility = () => {
      if (document.hidden) stopLoop();
      else startLoop();
    };
    document.addEventListener("visibilitychange", handleVisibility);

    const handleReducedChange = () => {
      reduced = reducedQuery.matches;
      if (reduced) {
        stopLoop();
        renderer.render(scene, camera);
      } else {
        startLoop();
      }
    };
    reducedQuery.addEventListener("change", handleReducedChange);

    const resize = () => {
      const { clientWidth, clientHeight } = canvas;
      if (clientWidth === 0 || clientHeight === 0) return;
      renderer.setSize(clientWidth, clientHeight, false);
      camera.aspect = clientWidth / clientHeight;
      camera.updateProjectionMatrix();
      if (reduced) renderer.render(scene, camera);
    };
    const resizeObserver = new ResizeObserver(resize);
    resizeObserver.observe(canvas);
    resize();

    if (reduced) {
      renderer.render(scene, camera);
    } else {
      startLoop();
    }

    const cleanup = () => {
      stopLoop();
      resizeObserver.disconnect();
      themeObserver.disconnect();
      document.removeEventListener("visibilitychange", handleVisibility);
      reducedQuery.removeEventListener("change", handleReducedChange);
      pointsGeometry.dispose();
      pointsMaterial.dispose();
      glowTexture.dispose();
      glowMaterial.dispose();
      ringGeometry.dispose();
      ringAMaterial.dispose();
      ringBMaterial.dispose();
      spokeGeometry.dispose();
      spokeMaterial.dispose();
      renderer.dispose();
      renderer.forceContextLoss();
    };
    return cleanup;
  }, []);

  return (
    <canvas
      ref={canvasRef}
      className="absolute inset-0 h-full w-full"
      aria-hidden="true"
    />
  );
}
