import { useQuery } from "@tanstack/react-query";
import { automationPath, controlRequest, type Automation } from "../lib/automation";
import { useNetwork } from "./useNetwork";

export function useRoomAutomation(roomID?: string) {
  const { data: network } = useNetwork();
  return useQuery({
    queryKey: ["automation", roomID],
    queryFn: () => controlRequest<Automation>(automationPath(roomID!)),
    enabled: !!roomID && !!network?.console?.can_manage_automation,
    refetchInterval: 3000,
  });
}
