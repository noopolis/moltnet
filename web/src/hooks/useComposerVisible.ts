import { isMessageTargetSelection } from "../lib/types";
import { useSelection } from "../providers";
import { useNetwork } from "./useNetwork";

export function useComposerVisible(): boolean {
  const { data: network } = useNetwork();
  const { selected } = useSelection();
  const ingressOn = !!network?.capabilities?.human_ingress;
  const canSendHuman = network?.console?.can_send_human ?? ingressOn;
  return ingressOn && canSendHuman && isMessageTargetSelection(selected);
}
