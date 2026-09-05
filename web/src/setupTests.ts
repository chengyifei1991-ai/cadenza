// 测试全局准备（node 与 jsdom 双环境安全）：
// - jsdom 用例：补齐 antd/rc- 需要的 matchMedia / ResizeObserver / getComputedStyle。
import "@testing-library/jest-dom/vitest";

if (typeof window !== "undefined") {
  // antd responsive 组件依赖 window.matchMedia
  if (!window.matchMedia) {
    Object.defineProperty(window, "matchMedia", {
      writable: true,
      value: (query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: () => undefined,
        removeListener: () => undefined,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
        dispatchEvent: () => false,
      }),
    });
  }
  // 部分组件（如 Select 弹层定位）需要 ResizeObserver
  if (!("ResizeObserver" in window)) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (window as any).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
}
