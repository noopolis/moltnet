import { useLexicalComposerContext } from "@lexical/react/LexicalComposerContext";
import { useMutation } from "@tanstack/react-query";
import {
  $getRoot,
  $isElementNode,
  $isLineBreakNode,
  $isTextNode,
  CLEAR_EDITOR_COMMAND,
  COMMAND_PRIORITY_HIGH,
  KEY_ENTER_COMMAND,
} from "lexical";
import { useEffect, useRef, useState } from "react";
import { useDMs } from "../../hooks/useDMs";
import { useNetwork } from "../../hooks/useNetwork";
import { ApiError, api, type SendMessageBody } from "../../lib/api";
import { supportsDirectMessages } from "../../lib/capabilities";
import {
  isMessageTargetSelection,
  type MessageTarget,
} from "../../lib/types";
import { useSelection } from "../../providers";
import { $isMentionNode } from "./MentionNode";

interface ExtractedMessage {
  text: string;
  mentions: string[];
}

function mentionMenuIsOpen(): boolean {
  return document.querySelector('[data-mention-menu="true"]') !== null;
}

function extractMessage(): ExtractedMessage {
  const lines: string[] = [];
  const mentions: string[] = [];

  const root = $getRoot();
  for (const block of root.getChildren()) {
    if (!$isElementNode(block)) continue;
    let line = "";
    for (const child of block.getChildren()) {
      if ($isMentionNode(child)) {
        mentions.push(child.getMentionName());
        line += child.getTextContent();
      } else if ($isTextNode(child)) {
        line += child.getTextContent();
      } else if ($isLineBreakNode(child)) {
        line += "\n";
      }
    }
    lines.push(line);
  }

  return { text: lines.join("\n"), mentions };
}

function formatSendError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 401) {
      return "Sending requires a console token with write scope.";
    }
    if (error.status === 403) {
      return error.message || "This console token cannot send here.";
    }
    return error.message || `Message send failed (${error.status}).`;
  }

  if (error instanceof Error && error.message.trim() !== "") {
    return error.message;
  }
  return "Message could not be sent.";
}

export function SubmitPlugin({ recipient = "" }: { recipient?: string }) {
  const [editor] = useLexicalComposerContext();
  const { selected } = useSelection();
  const { data: dms = [] } = useDMs();
  const { data: network } = useNetwork();
  const directMessagesEnabled = supportsDirectMessages(network);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const sending = useRef(false);
  const attempt = useRef<{ fingerprint: string; id: string }>();

  const sendMutation = useMutation({
    mutationFn: api.sendMessage,
    onSuccess: () => {
      attempt.current = undefined;
      setErrorMessage(null);
      editor.dispatchCommand(CLEAR_EDITOR_COMMAND, undefined);
    },
    onError: (error) => {
      setErrorMessage(formatSendError(error));
    },
    onSettled: () => { sending.current = false; },
  });

  useEffect(() => {
    return editor.registerCommand(
      KEY_ENTER_COMMAND,
      (event) => {
        if (event === null) return false;
        if (event.ctrlKey || event.metaKey || event.shiftKey) {
          // Allow newline insertion for these modifiers.
          return false;
        }
        if (mentionMenuIsOpen()) return false;
        if (!isMessageTargetSelection(selected)) return false;

        event.preventDefault();
        if (sending.current) return true;

        const { text, mentions } = editor
          .getEditorState()
          .read(() => extractMessage());
        const trimmed = text.trim();
        if (!trimmed) return true;
        if (selected.kind === "dm" && !directMessagesEnabled) return true;

        const target: MessageTarget =
          selected.kind === "room"
            ? { kind: "room", room_id: selected.id }
            : {
                kind: "dm",
                dm_id: selected.id,
                participant_ids:
                  dms.find((dm) => dm.id === selected.id)?.participant_ids ?? [],
              };

        const recipients = [...new Set([...mentions, ...(recipient ? [recipient] : [])])];
        const fingerprint = JSON.stringify({ target, text: trimmed, recipients });
        if (attempt.current?.fingerprint !== fingerprint) {
          attempt.current = { fingerprint, id: `msg_${crypto.randomUUID()}` };
        }
        const body: SendMessageBody = {
          id: attempt.current.id,
          target,
          from: { type: "human", id: "operator", name: "我" },
          parts: [{ kind: "text", text: trimmed }],
          ...(recipients.length > 0 ? { mentions: recipients } : {}),
        };

        setErrorMessage(null);
        sending.current = true;
        sendMutation.mutate(body);
        return true;
      },
      COMMAND_PRIORITY_HIGH,
    );
  }, [editor, selected, dms, sendMutation, directMessagesEnabled, recipient]);

  return (
    <div className="flex items-center justify-between gap-3">
      <span role="status" className="text-[11px] text-coral">{errorMessage}</span>
      <button type="button" aria-label="发送消息" disabled={sendMutation.isPending}
        className="rounded-md bg-green px-3 py-1 text-xs font-semibold text-bg disabled:opacity-50"
        onClick={() => editor.dispatchCommand(KEY_ENTER_COMMAND, new KeyboardEvent("keydown", { key: "Enter" }))}>
        {sendMutation.isPending ? "发送中…" : "发送"}
      </button>
    </div>
  );
}
