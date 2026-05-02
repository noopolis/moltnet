import {
  TextNode,
  type EditorConfig,
  type LexicalNode,
  type NodeKey,
  type SerializedTextNode,
  type Spread,
} from "lexical";

export type SerializedMentionNode = Spread<
  { mentionName: string; type: "mention"; version: 1 },
  SerializedTextNode
>;

export class MentionNode extends TextNode {
  __mention: string;

  static getType(): string {
    return "mention";
  }

  static clone(node: MentionNode): MentionNode {
    return new MentionNode(node.__mention, node.__text, node.__key);
  }

  static importJSON(serialized: SerializedMentionNode): MentionNode {
    const node = $createMentionNode(serialized.mentionName);
    node.setFormat(serialized.format);
    node.setDetail(serialized.detail);
    node.setMode(serialized.mode);
    node.setStyle(serialized.style);
    return node;
  }

  constructor(mentionName: string, text?: string, key?: NodeKey) {
    super(text ?? `@${mentionName}`, key);
    this.__mention = mentionName;
  }

  exportJSON(): SerializedMentionNode {
    return {
      ...super.exportJSON(),
      mentionName: this.__mention,
      type: "mention",
      version: 1,
    };
  }

  createDOM(config: EditorConfig): HTMLElement {
    const element = super.createDOM(config);
    element.style.color = "var(--color-green)";
    element.style.background = "var(--color-tint)";
    element.style.padding = "0 4px";
    element.style.borderRadius = "3px";
    return element;
  }

  isTextEntity(): true {
    return true;
  }

  isToken(): boolean {
    return true;
  }

  getMentionName(): string {
    return this.__mention;
  }
}

export function $createMentionNode(mentionName: string): MentionNode {
  const node = new MentionNode(mentionName);
  node.setMode("token");
  return node;
}

export function $isMentionNode(
  node: LexicalNode | null | undefined,
): node is MentionNode {
  return node instanceof MentionNode;
}
