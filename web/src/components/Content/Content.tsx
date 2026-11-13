import { useComposerVisible } from "../../hooks/useComposerVisible";
import { useSelection } from "../../providers";
import { Composer } from "../Composer";
import { AutomationPanel } from "../Automation/AutomationPanel";
import { AgentView } from "./AgentView";
import { EventsView } from "./EventsView";
import { PairingView } from "./PairingView";
import { Timeline } from "./Timeline";

export function Content() {
  const { selected } = useSelection();
  const showComposer = useComposerVisible();

  if (selected?.kind === "events") {
    return <EventsView />;
  }

  if (selected?.kind === "agent") {
    return <AgentView />;
  }

  if (selected?.kind === "pairing") {
    return <PairingView />;
  }

  return (
    <section className="flex min-h-0 min-w-0 flex-col">
      <AutomationPanel />
      <div className="grid min-h-0 flex-1 grid-rows-[minmax(0,1fr)]"><Timeline /></div>
      {showComposer ? <Composer /> : null}
    </section>
  );
}
