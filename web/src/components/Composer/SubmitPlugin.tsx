import { useLexicalComposerContext } from "@lexical/react/LexicalComposerContext";
import { useMutation } from "@tanstack/react-query";
import {
  $getRoot,
  $isElementNode,
  $isTextNode,
  CLEAR_EDITOR_COMMAND,
  COMMAND_PRIORITY_HIGH,
  KEY_ENTER_COMMAND,
} from "lexical";
import { useEffect, useState } from "react";
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

export function SubmitPlugin() {
  const [editor] = useLexicalComposerContext();
  const { selected } = useSelection();
  const { data: dms = [] } = useDMs();
  const { data: network } = useNetwork();
  const directMessagesEnabled = supportsDirectMessages(network);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const sendMutation = useMutation({
    mutationFn: api.sendMessage,
    onSuccess: () => {
      setErrorMessage(null);
      editor.dispatchCommand(CLEAR_EDITOR_COMMAND, undefined);
    },
    onError: (error) => {
      setErrorMessage(formatSendError(error));
    },
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

        const body: SendMessageBody = {
          target,
          from: { type: "human", id: "operator", name: "Operator" },
          parts: [{ kind: "text", text: trimmed }],
          ...(mentions.length > 0 ? { mentions } : {}),
        };

        setErrorMessage(null);
        sendMutation.mutate(body);
        return true;
      },
      COMMAND_PRIORITY_HIGH,
    );
  }, [editor, selected, dms, sendMutation, directMessagesEnabled]);

  if (!errorMessage) return null;

  return (
    <div role="status" className="mt-2 text-[11px] text-coral">
      {errorMessage}
    </div>
  );
}
