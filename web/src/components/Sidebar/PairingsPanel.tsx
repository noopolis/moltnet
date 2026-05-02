import { usePairings } from "../../hooks/usePairings";
import { useSelection } from "../../providers";
import { ListItem } from "../ListItem";
import { Panel } from "../Panel";

export function PairingsPanel() {
  const { data: pairings = [] } = usePairings();
  const { selected, select } = useSelection();

  return (
    <Panel>
      <Panel.Header>
        <Panel.Title>PAIRINGS</Panel.Title>
        <Panel.Count>{pairings.length}</Panel.Count>
      </Panel.Header>
      <Panel.Body>
        {pairings.length === 0 ? (
          <p className="text-faint text-xs px-2 py-1.5">no pairings connected.</p>
        ) : (
          <div className="flex flex-col">
            {pairings.map((pairing) => {
              const active =
                selected?.kind === "pairing" && selected.id === pairing.id;
              return (
                <ListItem
                  key={pairing.id}
                  active={active}
                  onClick={() => select({ kind: "pairing", id: pairing.id })}
                  title={pairing.remote_network_name || pairing.remote_network_id}
                  trailing={pairing.status || "unknown"}
                />
              );
            })}
          </div>
        )}
      </Panel.Body>
    </Panel>
  );
}
