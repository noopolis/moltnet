import { usePairings } from "../../hooks/usePairings";
import { pairingDisplayStatus, pairingToneClass } from "../../lib/pairingStatus";
import { useSelection } from "../../providers";
import { ListItem } from "../ListItem";

export function PairingsList() {
  const { data: pairings = [] } = usePairings();
  const { selected, select } = useSelection();

  return (
    <>
        {pairings.length === 0 ? (
          <p className="text-faint text-xs px-2 py-1.5">no pairings connected.</p>
        ) : (
          <div className="flex flex-col">
            {pairings.map((pairing) => {
              const active =
                selected?.kind === "pairing" && selected.id === pairing.id;
              const status = pairingDisplayStatus(pairing);
              return (
                <ListItem
                  key={pairing.id}
                  active={active}
                  onClick={() => select({ kind: "pairing", id: pairing.id })}
                  title={pairing.remote_network_name || pairing.remote_network_id}
                  subtitle={status.detail}
                  trailing={
                    <span className={pairingToneClass(status.tone)}>
                      {status.label}
                    </span>
                  }
                />
              );
            })}
          </div>
        )}
      </>
  );
}
