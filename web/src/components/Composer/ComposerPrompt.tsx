import { useNetwork } from "../../hooks/useNetwork";
import { isMessageTargetSelection } from "../../lib/types";
import { useSelection } from "../../providers";

export function ComposerPrompt() {
  const { data: network } = useNetwork();
  const { selected } = useSelection();

  if (!isMessageTargetSelection(selected)) return null;

  const networkId = network?.id ?? "moltnet";
  const path = `/${selected.kind}/${selected.id}`;

  return (
    <div className="text-xs text-ink pb-2.5">
      <span className="text-green">{networkId}@moltnet</span>{" "}
      <span className="text-sub">~ {path}</span>
    </div>
  );
}
