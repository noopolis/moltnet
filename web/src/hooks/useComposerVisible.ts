import { isMessageTargetSelection } from "../lib/types";
import { useSelection } from "../providers";
import { useNetwork } from "./useNetwork";

export function useComposerVisible(): boolean {
  const { data: network } = useNetwork();
  const { selected } = useSelection();
  const ingressOn = !!network?.capabilities?.human_ingress;
  return ingressOn && isMessageTargetSelection(selected);
}
