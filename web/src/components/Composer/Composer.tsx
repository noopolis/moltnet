import { LexicalComposer } from "@lexical/react/LexicalComposer";
import { useState } from "react";
import { useSelection } from "../../providers";
import { useAgents } from "../../hooks/useAgents";
import { useRooms } from "../../hooks/useRooms";
import { useRoomAutomation } from "../../hooks/useRoomAutomation";
import { roomRecipients } from "../../lib/recipients";
import { useComposerVisible } from "../../hooks/useComposerVisible";
import { ComposerEditor } from "./ComposerEditor";
import { ComposerKeys } from "./ComposerKeys";
import { MentionNode } from "./MentionNode";
import { theme } from "./theme";

const initialConfig = {
  namespace: "moltnet-composer",
  theme,
  nodes: [MentionNode],
  onError(error: Error) {
    // Surface lexical errors to the console; future step can route through telemetry.
    console.error("[lexical]", error);
  },
};

export function Composer() {
  const visible = useComposerVisible();
  const { selected } = useSelection();
  if (!visible || !selected || !("id" in selected)) return null;
  return <ComposerPanel key={`${selected.kind}:${selected.id}`} />;
}

function ComposerPanel() {
  const { selected } = useSelection();
  const { data: agents = [] } = useAgents();
  const { data: rooms = [] } = useRooms();
  const [recipient, setRecipient] = useState("");
  const roomId = selected?.kind === "room" ? selected.id : undefined;
  const { data: automation } = useRoomAutomation(roomId);
  const available = roomId ? roomRecipients(agents, rooms.find((room) => room.id === roomId)) : agents;
  const offline = available.filter((agent) => (!recipient || agent.id === recipient) && !agent.connected &&
    !automation?.connectors.some((hook) => hook.agent_id === agent.id && hook.enabled && hook.verified));

  return (
    <section className="px-5 pb-4 pt-1" aria-label="Human ingress">
      {roomId && <div className="flex items-center gap-3 pb-2 text-xs text-mute">
        <label className="flex items-center gap-2">回复对象
          <select aria-label="回复对象" value={recipient} onChange={(e) => setRecipient(e.target.value)}
            className="rounded-md border border-line bg-panel px-2 py-1 text-ink">
            <option value="">全部 AI（或用 @ 指定）</option>
            {available.map((agent) => <option key={agent.id} value={agent.id}>{agent.name || agent.id}</option>)}
          </select>
        </label>
        <span>消息在群内可见</span>
      </div>}
      <div className="rounded-xl border border-line bg-panel px-4 py-2.5 focus-within:border-green-dim">
        <LexicalComposer initialConfig={initialConfig}>
          <ComposerEditor recipient={recipient} />
        </LexicalComposer>
      </div>
      <ComposerKeys />
      {offline.length > 0 && <p className="pt-2 text-xs text-amber" role="status">
        {offline.map((agent) => agent.name || agent.id).join("、")} 尚未连接自动接收；消息会保存，需要在对应客户端让它读取。
      </p>}
    </section>
  );
}
