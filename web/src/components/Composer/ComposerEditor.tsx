import { ClearEditorPlugin } from "@lexical/react/LexicalClearEditorPlugin";
import { ContentEditable } from "@lexical/react/LexicalContentEditable";
import { LexicalErrorBoundary } from "@lexical/react/LexicalErrorBoundary";
import { HistoryPlugin } from "@lexical/react/LexicalHistoryPlugin";
import { PlainTextPlugin } from "@lexical/react/LexicalPlainTextPlugin";
import { MentionsPlugin } from "./MentionsPlugin";
import { SubmitPlugin } from "./SubmitPlugin";

export function ComposerEditor() {
  return (
    <div className="flex gap-2.5 items-start">
      <span className="text-green font-bold pt-1 select-none">{">"}</span>
      <div className="flex-1 relative min-w-0">
        <PlainTextPlugin
          contentEditable={
            <ContentEditable
              className="min-h-[28px] outline-none text-ink whitespace-pre-wrap break-words py-1"
              aria-label="Compose a message"
            />
          }
          placeholder={
            <span className="absolute top-1 left-0 text-faint pointer-events-none">
              Send a room or DM message into this network.
            </span>
          }
          ErrorBoundary={LexicalErrorBoundary}
        />
        <HistoryPlugin />
        <ClearEditorPlugin />
        <MentionsPlugin />
        <SubmitPlugin />
      </div>
    </div>
  );
}
