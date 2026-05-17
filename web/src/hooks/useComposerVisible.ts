import { isMessageTargetSelection } from "../lib/types";
import { useSelection } from "../providers";
import { useNetwork } from "./useNetwork";
import { useRooms } from "./useRooms";

export function useComposerVisible(): boolean {
  const { data: network } = useNetwork();
  const { data: rooms = [] } = useRooms();
  const { selected } = useSelection();
  const ingressOn = !!network?.capabilities?.human_ingress;
  const canSendHuman = network?.console?.can_send_human ?? ingressOn;
  if (!ingressOn || !canSendHuman || !isMessageTargetSelection(selected)) {
    return false;
  }
  if (selected.kind === "room") {
    const room = rooms.find((item) => item.id === selected.id);
    return room?.access?.can_write === true;
  }
  return true;
}
