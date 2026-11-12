import { ClearEditorPlugin } from "@lexical/react/LexicalClearEditorPlugin";
import { ContentEditable } from "@lexical/react/LexicalContentEditable";
import { LexicalErrorBoundary } from "@lexical/react/LexicalErrorBoundary";
import { HistoryPlugin } from "@lexical/react/LexicalHistoryPlugin";
import { PlainTextPlugin } from "@lexical/react/LexicalPlainTextPlugin";
import { MentionsPlugin } from "./MentionsPlugin";
import { SubmitPlugin } from "./SubmitPlugin";

export function ComposerEditor({ recipient = "" }: { recipient?: string }) {
  return (
    <div className="flex gap-2.5 items-start">
      <div className="flex-1 relative min-w-0">
        <PlainTextPlugin
          contentEditable={
            <ContentEditable
              data-composer-input="true"
              className="min-h-[24px] outline-none text-ink text-[14.5px] whitespace-pre-wrap break-words py-0.5"
              aria-label="输入消息"
            />
          }
          placeholder={
            <span className="absolute top-0.5 left-0 text-faint text-[14.5px] pointer-events-none">
              输入消息，Enter 发送，Shift+Enter 换行…
            </span>
          }
          ErrorBoundary={LexicalErrorBoundary}
        />
        <HistoryPlugin />
        <ClearEditorPlugin />
        <MentionsPlugin />
        <SubmitPlugin recipient={recipient} />
      </div>
    </div>
  );
}
