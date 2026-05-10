import { useAgents } from "../../hooks/useAgents";
import { useSelection } from "../../providers";
import { ListItem } from "../ListItem";
import { Panel } from "../Panel";

export function AgentsPanel() {
  const { data: agents = [] } = useAgents();
  const { selected, select } = useSelection();

  return (
    <Panel>
      <Panel.Header>
        <Panel.Title>AGENTS</Panel.Title>
        <Panel.Count>{agents.length}</Panel.Count>
      </Panel.Header>
      <Panel.Body>
        {agents.length === 0 ? (
          <p className="text-faint text-xs px-2 py-1.5">no agents connected.</p>
        ) : (
          <div className="flex flex-col">
            {agents.map((agent) => {
              const active =
                selected?.kind === "agent" && selected.id === agent.id;
              const connected = agent.connected === true;
              return (
                <ListItem
                  key={agent.id}
                  active={active}
                  onClick={() => select({ kind: "agent", id: agent.id })}
                  title={agent.id}
                  marker={
                    <span
                      className={`inline-block h-[7px] w-[7px] rounded-full ${
                        connected ? "bg-green animate-breathe" : "bg-coral"
                      }`}
                    />
                  }
                  markerClassName={connected ? "text-green" : "text-coral"}
                  trailing={
                    (agent.rooms ?? []).length > 0
                      ? `${agent.rooms!.length} rooms`
                      : undefined
                  }
                />
              );
            })}
          </div>
        )}
      </Panel.Body>
    </Panel>
  );
}
