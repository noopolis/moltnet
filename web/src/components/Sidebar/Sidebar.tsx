import { AgentsPanel } from "./AgentsPanel";
import { DirectChannelsPanel } from "./DirectChannelsPanel";
import { PairingsPanel } from "./PairingsPanel";
import { RoomsPanel } from "./RoomsPanel";

export function Sidebar() {
  return (
    <aside className="grid gap-4 min-h-0 grid-rows-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,0.7fr)]">
      <RoomsPanel />
      <DirectChannelsPanel />
      <AgentsPanel />
      <PairingsPanel />
    </aside>
  );
}
