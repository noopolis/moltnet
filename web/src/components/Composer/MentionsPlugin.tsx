import { useLexicalComposerContext } from "@lexical/react/LexicalComposerContext";
import {
  LexicalTypeaheadMenuPlugin,
  MenuOption,
  useBasicTypeaheadTriggerMatch,
} from "@lexical/react/LexicalTypeaheadMenuPlugin";
import { COMMAND_PRIORITY_HIGH, type TextNode } from "lexical";
import {
  type CSSProperties,
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { useAgents } from "../../hooks/useAgents";
import { useRooms } from "../../hooks/useRooms";
import type { Agent } from "../../lib/types";
import { useSelection } from "../../providers";
import { $createMentionNode } from "./MentionNode";

const MAX_RESULTS = 5;
const MENU_GAP_PX = 6;
const MENU_MARGIN_PX = 8;
const MENU_MAX_HEIGHT_PX = 360;
const MENU_MIN_WIDTH_PX = 220;
const MENU_MAX_WIDTH_PX = 360;
const LEXICAL_ANCHOR_OFFSET_PX = 3;

type MenuPlacement = "up" | "down";

class MentionMenuOption extends MenuOption {
  agent: Agent;
  constructor(agent: Agent) {
    super(agent.id);
    this.agent = agent;
  }
}

function clamp(value: number, min: number, max: number): number {
  if (max < min) return min;
  return Math.min(Math.max(value, min), max);
}

interface MentionMenuProps {
  anchorElement: HTMLElement;
  options: MentionMenuOption[];
  selectedIndex: number | null;
  selectOptionAndCleanUp: (option: MentionMenuOption) => void;
  setHighlightedIndex: (index: number) => void;
}

function MentionMenu({
  anchorElement,
  options,
  selectedIndex,
  selectOptionAndCleanUp,
  setHighlightedIndex,
}: MentionMenuProps) {
  const menuRef = useRef<HTMLDivElement | null>(null);
  const placementRef = useRef<MenuPlacement | null>(null);
  const [style, setStyle] = useState<CSSProperties>({
    left: 0,
    maxHeight: MENU_MAX_HEIGHT_PX,
    position: "fixed",
    top: 0,
    visibility: "hidden",
    width: MENU_MAX_WIDTH_PX,
    zIndex: 60,
  });

  useLayoutEffect(() => {
    const menu = menuRef.current;
    if (!menu) return undefined;

    const updatePosition = () => {
      const anchorRect = anchorElement.getBoundingClientRect();
      const viewportWidth = window.innerWidth;
      const viewportHeight = window.innerHeight;
      const width = Math.min(
        MENU_MAX_WIDTH_PX,
        Math.max(MENU_MIN_WIDTH_PX, viewportWidth - MENU_MARGIN_PX * 2),
      );
      const textTop = Math.max(
        MENU_MARGIN_PX,
        anchorRect.top - anchorRect.height - LEXICAL_ANCHOR_OFFSET_PX,
      );
      const textBottom = Math.max(
        textTop,
        anchorRect.top - LEXICAL_ANCHOR_OFFSET_PX,
      );
      const availableAbove = Math.max(
        0,
        textTop - MENU_MARGIN_PX - MENU_GAP_PX,
      );
      const availableBelow = Math.max(
        0,
        viewportHeight - textBottom - MENU_MARGIN_PX - MENU_GAP_PX,
      );
      const naturalHeight = Math.min(menu.scrollHeight, MENU_MAX_HEIGHT_PX);
      const placement =
        placementRef.current ??
        (availableAbove > availableBelow ? "up" : "down");
      placementRef.current = placement;
      const openUp = placement === "up";
      const maxHeight = Math.max(
        96,
        Math.min(MENU_MAX_HEIGHT_PX, openUp ? availableAbove : availableBelow),
      );
      const height = Math.min(naturalHeight, maxHeight);
      const top = openUp
        ? Math.max(MENU_MARGIN_PX, textTop - height - MENU_GAP_PX)
        : Math.min(
            textBottom + MENU_GAP_PX,
            viewportHeight - MENU_MARGIN_PX - height,
          );

      setStyle({
        left: clamp(
          anchorRect.left,
          MENU_MARGIN_PX,
          viewportWidth - width - MENU_MARGIN_PX,
        ),
        maxHeight,
        overflowY: naturalHeight > maxHeight ? "auto" : "hidden",
        position: "fixed",
        top,
        visibility: "visible",
        width,
        zIndex: 60,
      });
    };

    updatePosition();
    const frame = window.requestAnimationFrame(updatePosition);
    window.addEventListener("resize", updatePosition);
    document.addEventListener("scroll", updatePosition, {
      capture: true,
      passive: true,
    });
    const observer = new ResizeObserver(updatePosition);
    observer.observe(menu);

    return () => {
      window.cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener("resize", updatePosition);
      document.removeEventListener("scroll", updatePosition, true);
    };
  }, [anchorElement, options.length]);

  return (
    <div
      ref={menuRef}
      role="listbox"
      className="bg-bg border border-line rounded shadow-lg p-1 overflow-auto"
      style={style}
      onMouseDown={(event) => event.preventDefault()}
    >
      {options.map((option, i) => (
        <div
          key={option.agent.id}
          ref={option.setRefElement}
          role="option"
          aria-selected={i === selectedIndex}
          tabIndex={-1}
          title={option.agent.fqid ?? option.agent.id}
          className={`px-2.5 py-1.5 cursor-pointer text-xs rounded ${
            i === selectedIndex ? "bg-tint text-green" : "text-sub"
          }`}
          onClick={() => selectOptionAndCleanUp(option)}
          onMouseEnter={() => setHighlightedIndex(i)}
        >
          <span className="block font-semibold break-words leading-relaxed">
            @{option.agent.id}
          </span>
          {option.agent.fqid ? (
            <span className="block text-faint text-[11px] break-all">
              {option.agent.fqid}
            </span>
          ) : null}
        </div>
      ))}
    </div>
  );
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
      commandPriority={COMMAND_PRIORITY_HIGH}
      menuRenderFn={(
        anchorElementRef,
        { selectedIndex, selectOptionAndCleanUp, setHighlightedIndex },
      ) => {
        if (!anchorElementRef.current || options.length === 0) return null;
        return createPortal(
          <MentionMenu
            anchorElement={anchorElementRef.current}
            options={options}
            selectedIndex={selectedIndex}
            selectOptionAndCleanUp={selectOptionAndCleanUp}
            setHighlightedIndex={setHighlightedIndex}
          />,
          document.body,
        );
      }}
    />
  );
}
