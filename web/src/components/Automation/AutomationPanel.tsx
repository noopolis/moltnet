import { useState } from "react";
import { useRoomAutomation } from "../../hooks/useRoomAutomation";
import { useAgents } from "../../hooks/useAgents";
import { useNetwork } from "../../hooks/useNetwork";
import { useRooms } from "../../hooks/useRooms";
import { automationPath, controlRequest, type WakeMode } from "../../lib/automation";
import { useSelection } from "../../providers";
import { WebhookForm } from "./WebhookForm";

export function AutomationPanel() {
  const { selected } = useSelection();
  const { data: network } = useNetwork();
  const room = selected?.kind === "room" ? selected.id : "";
  if (!room || !network?.console?.can_manage_automation) return null;
  return <RoomControls key={room} roomID={room} />;
}

function RoomControls({ roomID }: { roomID: string }) {
  const { data: rooms = [] } = useRooms();
  const { data: agents = [] } = useAgents();
  const room = rooms.find((item) => item.id === roomID);
  const members = agents.filter((agent) => room?.members?.includes(agent.id));
  const path = automationPath(roomID);
  const query = useRoomAutomation(roomID);
  const [topic, setTopic] = useState("");
  const [rounds, setRounds] = useState(3);
  const [chosen, setChosen] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState("");
  const discussion = query.data?.discussion;
  async function action(url: string, method: string, body: unknown) {
    setBusy(true); setFeedback("");
    try { await controlRequest(url, method, body); await query.refetch(); }
    catch (error) { setFeedback(error instanceof Error ? error.message : "操作失败"); }
    finally { setBusy(false); }
  }
  return (
    <details className="shrink-0 border-b border-border bg-panel px-4 py-2 text-ink">
      <summary className="cursor-pointer text-sm font-medium">AI 设置与讨论
        <span className="ml-3 text-xs font-normal text-mute">{discussion ? `${discussion.active ? "讨论中" : "讨论已结束"} · 已发言 ${discussion.completed_turns}/${discussion.max_rounds * discussion.participants.length} 次` : "按需回应 · 可开启有限轮次讨论"}</span>
      </summary>
      <div className="max-h-[30vh] overflow-auto py-3">
        {query.isLoading ? <p role="status">正在读取 AI 设置…</p> : null}
        {query.error ? <p role="alert">{query.error.message}<button onClick={() => void query.refetch()} className="ml-2 underline">重试</button></p> : null}
        {query.data ? <>
          <p className="mb-3 text-xs text-mute">每个 AI 只需接入一次。平时按下面的规则回应；开始讨论后按勾选顺序逐个发言，每轮每个 AI 一次。新消息会中止讨论。</p>
          <div className="grid gap-3 md:grid-cols-2">
            {members.map((agent) => {
              const hook = query.data.connectors.find((item) => item.agent_id === agent.id);
              const mode = query.data.settings.find((item) => item.agent_id === agent.id)?.mode ?? "owner";
              return <section key={agent.id} className="rounded border border-border p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <label className="flex items-center gap-2 text-sm">
                    <input type="checkbox" aria-label={`让 ${agent.name} 参加讨论`} disabled={busy || !!discussion?.active} checked={chosen.includes(agent.id)} onChange={(e) => setChosen((ids) => e.target.checked ? [...ids, agent.id] : ids.filter((id) => id !== agent.id))} />
                    {agent.name}
                  </label>
                  <span className="text-xs text-mute">{hook?.verified && hook.enabled ? "Webhook 已验证" : agent.connected ? "监听已连接" : hook?.configured ? "Webhook 待测试或已停用" : "未接通自动唤醒"}</span>
                </div>
                <label className="mt-2 flex flex-wrap items-center gap-2 text-xs">平时唤醒方式
                  <select aria-label={`${agent.name} 的唤醒方式`} disabled={busy || !!discussion?.active} value={mode} onChange={(e) => void action(`${path}/agents/${encodeURIComponent(agent.id)}`, "PUT", { mode: e.target.value as WakeMode })} className="rounded border border-border bg-bg px-2 py-1">
                    <option value="owner">只回应我的消息</option><option value="mentions">仅被 @ 时回应（含其他 AI）</option><option value="never">暂停自动回应</option>
                  </select>
                </label>
                {!agent.connected || hook?.configured ? <WebhookForm key={agent.id} room={roomID} agent={agent.id} status={hook} refresh={query.refetch} /> : null}
              </section>;
            })}
          </div>
          {members.length === 0 ? <p className="text-sm text-mute">房间还没有 AI，请先接入并添加为房间成员。</p> : null}
          <form className="mt-4 grid gap-2" onSubmit={(event) => { event.preventDefault(); void action(path + "/discussion", "POST", { topic, participants: chosen, max_rounds: rounds }); }}>
            <label className="text-sm">讨论主题
              <textarea aria-label="讨论主题" required maxLength={2000} disabled={busy || !!discussion?.active} value={topic} onChange={(e) => setTopic(e.target.value)} placeholder="例如：讨论这个方案的优缺点，最后给我建议" rows={2} className="mt-1 w-full rounded border border-border bg-bg p-2" />
            </label>
            <div className="flex flex-wrap items-center gap-3 text-sm">
              <label>最多 <input aria-label="最大讨论轮数" type="number" min={1} max={20} required disabled={busy || !!discussion?.active} value={rounds} onChange={(e) => setRounds(Number(e.target.value))} className="w-14 rounded border border-border bg-bg px-2 py-1" /> 轮</label>
              <button disabled={busy || !!discussion?.active || chosen.length < 2} className="rounded bg-ink px-3 py-1.5 text-bg disabled:opacity-40">开始讨论</button>
              <button type="button" disabled={busy || !discussion?.active} onClick={() => void action(path + "/stop", "POST", {})} className="rounded border border-border px-3 py-1.5 disabled:opacity-40">停止讨论</button>
            </div>
            <p className="text-xs text-mute">接力顺序：{chosen.map((id) => members.find((a) => a.id === id)?.name ?? id).join(" → ") || "请勾选至少两个 AI"}。处理消息会消耗相应模型额度。停止后不再唤醒下一位，已开始的运行可能仍会完成。</p>
          </form>
          {discussion ? <p role="status" className="mt-2 text-xs text-sub">{discussion.active ? `正在等待 ${discussion.expected_agent} 回复；超过 10 分钟未回复将停止接力。` : discussion.reason}</p> : null}
        </> : null}
        <p role="alert" className="mt-2 text-sm text-sub">{feedback}</p>
      </div>
    </details>
  );
}
