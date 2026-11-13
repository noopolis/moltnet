import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, expect, it, vi } from "vitest";
import { AutomationPanel } from "../components/Automation/AutomationPanel";
import { controlRequest, type Automation } from "../lib/automation";

const state = vi.hoisted(() => ({ owner: true }));
vi.mock("../providers", () => ({ useSelection: () => ({ selected: { kind: "room", id: "general" } }) }));
vi.mock("../hooks/useNetwork", () => ({ useNetwork: () => ({ data: { console: { can_manage_automation: state.owner } } }) }));
vi.mock("../hooks/useRooms", () => ({ useRooms: () => ({ data: [{ id: "general", members: ["codex", "grok"] }] }) }));
vi.mock("../hooks/useAgents", () => ({ useAgents: () => ({ data: [{ id: "codex", name: "Codex", connected: true }, { id: "grok", name: "Grok", connected: true }] }) }));
vi.mock("../lib/automation", async (original) => ({ ...await original<typeof import("../lib/automation")>(), controlRequest: vi.fn() }));
let view: Automation;
beforeEach(() => {
  state.owner = true;
  view = { settings: [], connectors: [] };
  vi.mocked(controlRequest).mockReset().mockImplementation(async (_url, method) => {
    if (method === "POST") throw new Error("Grok 未接通自动唤醒");
    return view as never;
  });
});
function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><AutomationPanel /></QueryClientProvider>);
}
it("requires two selected agents, preserves turn order and shows server rejection", async () => {
  mount();
  const grok = await screen.findByLabelText("让 Grok 参加讨论");
  const start = screen.getByRole("button", { name: "开始讨论" });
  expect((start as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(grok);
  expect((start as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByLabelText("让 Codex 参加讨论"));
  fireEvent.change(screen.getByLabelText("讨论主题"), { target: { value: "比较方案" } });
  fireEvent.change(screen.getByLabelText("最大讨论轮数"), { target: { value: "2" } });
  fireEvent.submit(start.closest("form")!);
  await screen.findByText("Grok 未接通自动唤醒");
  expect(controlRequest).toHaveBeenCalledWith("/v1/rooms/general/automation/discussion", "POST", { topic: "比较方案", participants: ["grok", "codex"], max_rounds: 2 });
  expect((start as HTMLButtonElement).disabled).toBe(false);
});
it("locks running discussion settings and stops the server discussion", async () => {
  view.discussion = { id: "d", topic: "test", active: true, participants: ["codex", "grok"], completed_turns: 1, max_rounds: 2, expected_agent: "grok" };
  vi.mocked(controlRequest).mockImplementation(async (_url, method) => {
    if (method === "POST") view = { ...view, discussion: { ...view.discussion!, active: false, reason: "主人已停止讨论" } };
    return view as never;
  });
  mount();
  await screen.findByText(/正在等待 grok/);
  expect((screen.getByLabelText("讨论主题") as HTMLTextAreaElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "停止讨论" }));
  await screen.findByText("主人已停止讨论");
  await waitFor(() => expect((screen.getByLabelText("讨论主题") as HTMLTextAreaElement).disabled).toBe(false));
});
it("hides owner controls and performs no request for observers", () => {
  state.owner = false;
  const result = mount();
  expect(result.container.textContent).toBe("");
  expect(controlRequest).not.toHaveBeenCalled();
});
