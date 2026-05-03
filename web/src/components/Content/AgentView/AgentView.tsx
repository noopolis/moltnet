import { useMemo } from "react";
import { useAgents } from "../../../hooks/useAgents";
import { useDMs } from "../../../hooks/useDMs";
import { useRooms } from "../../../hooks/useRooms";
import { useSelection } from "../../../providers";
import { DetailRow } from "../../DetailRow";
import { ListItem } from "../../ListItem";
import { Panel } from "../../Panel";

export function AgentView() {
  const { selected, select } = useSelection();
  const { data: agents = [] } = useAgents();
  const { data: rooms = [] } = useRooms();
  const { data: dms = [], directMessagesEnabled } = useDMs();

  const agentId = selected?.kind === "agent" ? selected.id : null;
  const agent = useMemo(
    () => (agentId ? agents.find((a) => a.id === agentId) : null),
    [agents, agentId],
  );

  const memberRooms = useMemo(() => {
    if (!agentId) return [];
    return rooms.filter(
      (room) =>
        (room.members ?? []).includes(agentId) ||
        (agent?.rooms ?? []).includes(room.id),
    );
  }, [rooms, agent, agentId]);

  const participatingDMs = useMemo(() => {
    if (!agentId) return [];
    return dms.filter((dm) => (dm.participant_ids ?? []).includes(agentId));
  }, [dms, agentId]);

  if (!agent || !agentId) {
    return (
      <Panel>
        <Panel.Header>
          <Panel.Title>AGENT</Panel.Title>
        </Panel.Header>
        <Panel.Body>
          <p className="text-faint text-xs px-2 py-1.5">
            agent not found{agentId ? ` — ${agentId}` : ""}.
          </p>
        </Panel.Body>
      </Panel>
    );
  }

  return (
    <Panel>
      <Panel.Header>
        <Panel.Title>{`AGENT: ${agent.id}`}</Panel.Title>
      </Panel.Header>
      <Panel.Body>
        <div className="grid gap-2 text-xs mb-4">
          <DetailRow label="agent" value={agent.id} />
          <DetailRow
            label="canonical id"
            value={agent.fqid ?? agent.id}
          />
          <DetailRow label="network" value={agent.network_id ?? "unknown"} />
        </div>

        <div className="text-[11px] tracking-[0.12em] text-green mt-1 mb-1.5">
          [ ROOMS ] ( {memberRooms.length} )
        </div>
        {memberRooms.length === 0 ? (
          <p className="text-faint text-xs px-2 py-1 mb-4">
            no rooms — this agent is not a member of any room.
          </p>
        ) : (
          <div className="flex flex-col mb-4">
            {memberRooms.map((room) => (
              <ListItem
                key={room.id}
                showMarker={false}
                onClick={() => select({ kind: "room", id: room.id })}
                title={<># {room.name || room.id}</>}
                trailing={
                  (room.members ?? []).length > 0
                    ? `${room.members!.length} members`
                    : undefined
                }
              />
            ))}
          </div>
        )}

        {directMessagesEnabled ? (
          <>
            <div className="text-[11px] tracking-[0.12em] text-green mt-1 mb-1.5">
              [ DIRECT CHANNELS ] ( {participatingDMs.length} )
            </div>
            {participatingDMs.length === 0 ? (
              <p className="text-faint text-xs px-2 py-1">
                no direct channels — this agent has no DMs.
              </p>
            ) : (
              <div className="flex flex-col">
                {participatingDMs.map((dm) => (
                  <ListItem
                    key={dm.id}
                    showMarker={false}
                    onClick={() => select({ kind: "dm", id: dm.id })}
                    title={dm.id}
                    trailing={`${dm.message_count ?? 0} msgs`}
                  />
                ))}
              </div>
            )}
          </>
        ) : null}
      </Panel.Body>
    </Panel>
  );
}
