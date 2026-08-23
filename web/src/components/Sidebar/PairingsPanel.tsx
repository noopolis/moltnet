import type { Pairing } from "../../lib/types";
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
                  // The pairing id is the one label this operator authored
                  // and can act on: remote_network_name/id are the peer's own
                  // claims, and on the inviting side both are empty until the
                  // peer makes contact — which rendered a blank row.
                  title={pairing.id}
                  subtitle={pairingPeerLabel(pairing) ?? status.detail}
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

/**
 * What the peer says it is, marked as a claim. Empty on the inviting side
 * until the peer has actually connected, which is why it can never be the
 * primary label.
 */
function pairingPeerLabel(pairing: Pairing): string | undefined {
  const claimed = pairing.remote_network_name?.trim() || pairing.remote_network_id?.trim();
  return claimed ? `peer: ${claimed}` : undefined;
}
