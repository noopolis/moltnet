import { useComposerVisible } from "../../hooks/useComposerVisible";
import { useSelection } from "../../providers";
import { Composer } from "../Composer";
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
    <section
      className={
        showComposer
          ? "grid gap-4 min-h-0 grid-rows-[minmax(0,1fr)_auto]"
          : "grid min-h-0 grid-rows-[minmax(0,1fr)]"
      }
    >
      <Timeline />
      {showComposer ? <Composer /> : null}
    </section>
  );
}
