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
import { useEffect } from "react";
import { useDMs } from "../../hooks/useDMs";
import { useNetwork } from "../../hooks/useNetwork";
import { api, type SendMessageBody } from "../../lib/api";
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

export function SubmitPlugin() {
  const [editor] = useLexicalComposerContext();
  const { selected } = useSelection();
  const { data: dms = [] } = useDMs();
  const { data: network } = useNetwork();
  const directMessagesEnabled =
    !!network && network.capabilities?.direct_messages !== false;

  const sendMutation = useMutation({
    mutationFn: api.sendMessage,
    onSuccess: () => {
      editor.dispatchCommand(CLEAR_EDITOR_COMMAND, undefined);
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

        sendMutation.mutate(body);
        return true;
      },
      COMMAND_PRIORITY_HIGH,
    );
  }, [editor, selected, dms, sendMutation, directMessagesEnabled]);

  return null;
}
