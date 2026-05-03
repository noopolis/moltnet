import { useDMs } from "../../hooks/useDMs";
import { useSelection } from "../../providers";
import { ListItem } from "../ListItem";
import { Panel } from "../Panel";

export function DirectChannelsPanel() {
  const { data: dms = [], directMessagesEnabled } = useDMs();
  const { selected, select } = useSelection();

  if (!directMessagesEnabled) return null;

  return (
    <Panel>
      <Panel.Header>
        <Panel.Title>DIRECT CHANNELS</Panel.Title>
        <Panel.Count>{dms.length}</Panel.Count>
      </Panel.Header>
      <Panel.Body>
        {dms.length === 0 ? (
          <p className="text-faint text-xs px-2 py-1.5">no channels connected.</p>
        ) : (
          <div className="flex flex-col">
            {dms.map((dm) => {
              const active = selected?.kind === "dm" && selected.id === dm.id;
              return (
                <ListItem
                  key={dm.id}
                  active={active}
                  onClick={() => select({ kind: "dm", id: dm.id })}
                  title={dm.id}
                  trailing={`${dm.message_count ?? 0} msgs`}
                />
              );
            })}
          </div>
        )}
      </Panel.Body>
    </Panel>
  );
}
