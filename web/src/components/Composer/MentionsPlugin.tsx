import { useLexicalComposerContext } from "@lexical/react/LexicalComposerContext";
import {
  LexicalTypeaheadMenuPlugin,
  MenuOption,
  useBasicTypeaheadTriggerMatch,
} from "@lexical/react/LexicalTypeaheadMenuPlugin";
import { type TextNode } from "lexical";
import { useCallback, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { useAgents } from "../../hooks/useAgents";
import { useRooms } from "../../hooks/useRooms";
import type { Agent } from "../../lib/types";
import { useSelection } from "../../providers";
import { $createMentionNode } from "./MentionNode";

const MAX_RESULTS = 8;

class MentionMenuOption extends MenuOption {
  agent: Agent;
  constructor(agent: Agent) {
    super(agent.id);
    this.agent = agent;
  }
}

export function MentionsPlugin() {
  const [editor] = useLexicalComposerContext();
  const [queryString, setQueryString] = useState<string | null>(null);
  const { data: agents = [] } = useAgents();
  const { data: rooms = [] } = useRooms();
  const { selected } = useSelection();

  const eligibleAgents = useMemo<Agent[]>(() => {
    if (!selected) return agents;
    if (selected.kind !== "room") return agents;
    const room = rooms.find((r) => r.id === selected.id);
    if (!room) return [];
    const memberIds = new Set(room.members ?? []);
    return agents.filter(
      (a) => memberIds.has(a.id) || (a.rooms ?? []).includes(selected.id),
    );
  }, [agents, rooms, selected]);

  const options = useMemo<MentionMenuOption[]>(() => {
    if (queryString === null) return [];
    const lower = queryString.toLowerCase();
    return eligibleAgents
      .filter(
        (agent) =>
          agent.id.toLowerCase().includes(lower) ||
          (agent.fqid?.toLowerCase().includes(lower) ?? false),
      )
      .slice(0, MAX_RESULTS)
      .map((agent) => new MentionMenuOption(agent));
  }, [eligibleAgents, queryString]);

  const triggerFn = useBasicTypeaheadTriggerMatch("@", { minLength: 0 });

  const onSelectOption = useCallback(
    (
      option: MentionMenuOption,
      nodeToReplace: TextNode | null,
      closeMenu: () => void,
    ) => {
      editor.update(() => {
        const mentionNode = $createMentionNode(option.agent.id);
        if (nodeToReplace) {
          nodeToReplace.replace(mentionNode);
        }
        mentionNode.select();
        closeMenu();
      });
    },
    [editor],
  );

  return (
    <LexicalTypeaheadMenuPlugin
      onQueryChange={setQueryString}
      onSelectOption={onSelectOption}
      triggerFn={triggerFn}
      options={options}
      menuRenderFn={(
        anchorElementRef,
        { selectedIndex, selectOptionAndCleanUp, setHighlightedIndex },
      ) => {
        if (!anchorElementRef.current || options.length === 0) return null;
        return createPortal(
          <div className="bg-bg border border-line rounded shadow-lg p-1 min-w-[220px] max-h-64 overflow-auto">
            {options.map((option, i) => (
              <div
                key={option.agent.id}
                ref={option.setRefElement}
                role="option"
                aria-selected={i === selectedIndex}
                tabIndex={-1}
                className={`px-2.5 py-1.5 cursor-pointer text-xs rounded ${
                  i === selectedIndex ? "bg-tint text-green" : "text-sub"
                }`}
                onClick={() => selectOptionAndCleanUp(option)}
                onMouseEnter={() => setHighlightedIndex(i)}
              >
                <span className="font-semibold">@{option.agent.id}</span>
                {option.agent.fqid ? (
                  <span className="text-faint ml-2">{option.agent.fqid}</span>
                ) : null}
              </div>
            ))}
          </div>,
          anchorElementRef.current,
        );
      }}
    />
  );
}
