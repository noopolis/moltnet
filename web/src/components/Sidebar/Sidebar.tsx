import { useNetwork } from "../../hooks/useNetwork";
import { AgentsPanel } from "./AgentsPanel";
import { DirectChannelsPanel } from "./DirectChannelsPanel";
import { PairingsPanel } from "./PairingsPanel";
import { RoomsPanel } from "./RoomsPanel";

export function Sidebar() {
  const { data: network } = useNetwork();
  const directMessagesEnabled =
    !!network && network.capabilities?.direct_messages !== false;
  const gridRows = directMessagesEnabled
    ? "grid-rows-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,0.7fr)]"
    : "grid-rows-[minmax(0,1.5fr)_minmax(0,1fr)_minmax(0,0.7fr)]";

  return (
    <aside className={`grid gap-4 min-h-0 ${gridRows}`}>
      <RoomsPanel />
      {directMessagesEnabled ? <DirectChannelsPanel /> : null}
      <AgentsPanel />
      <PairingsPanel />
    </aside>
  );
}
