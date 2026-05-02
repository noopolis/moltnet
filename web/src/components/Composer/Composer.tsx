import { LexicalComposer } from "@lexical/react/LexicalComposer";
import { useComposerVisible } from "../../hooks/useComposerVisible";
import { Panel } from "../Panel";
import { ComposerEditor } from "./ComposerEditor";
import { ComposerKeys } from "./ComposerKeys";
import { ComposerPrompt } from "./ComposerPrompt";
import { MentionNode } from "./MentionNode";
import { theme } from "./theme";

const initialConfig = {
  namespace: "moltnet-composer",
  theme,
  nodes: [MentionNode],
  onError(error: Error) {
    // Surface lexical errors to the console; future step can route through telemetry.
    console.error("[lexical]", error);
  },
};

export function Composer() {
  if (!useComposerVisible()) return null;

  return (
    <Panel>
      <Panel.Header>
        <Panel.Title>HUMAN INGRESS</Panel.Title>
      </Panel.Header>
      <Panel.Body>
        <ComposerPrompt />
        <LexicalComposer initialConfig={initialConfig}>
          <ComposerEditor />
        </LexicalComposer>
      </Panel.Body>
      <Panel.Footer>
        <ComposerKeys />
      </Panel.Footer>
    </Panel>
  );
}
