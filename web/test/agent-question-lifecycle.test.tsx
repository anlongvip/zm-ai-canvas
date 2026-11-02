import { expect, test } from "bun:test";
import { resolve } from "node:path";

// Motion caches browser availability at import time; keep the DOM setup and its
// module cache in a child process so later SSR tests do not inherit that state.
test("真实 Agent 提问生命周期在独立 DOM 环境中通过", () => {
    const originalWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
    const result = Bun.spawnSync([process.execPath, "test", "./test/fixtures/agent-question-lifecycle.harness.tsx"], { cwd: resolve(import.meta.dir, ".."), timeout: 20000 });
    const output = new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr);
    expect(result.exitCode, output).toBe(0);
    expect(output).toContain("5 pass");
    expect(output).toContain("0 fail");
    expect(Object.getOwnPropertyDescriptor(globalThis, "window")).toEqual(originalWindow);
}, 25000);
