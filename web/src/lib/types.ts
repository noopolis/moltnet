export interface NetworkCapabilities {
  event_stream?: string;
  message_pagination?: string;
  human_ingress?: boolean;
  direct_messages?: boolean;
  pairings?: boolean;
  attachment_protocol?: string;
}

export interface Network {
  id: string;
  name: string;
  version: string;
  capabilities: NetworkCapabilities;
}

export interface Room {
  id: string;
  name: string;
  fqid?: string;
  members?: string[];
}

export interface DirectChannel {
  id: string;
  fqid?: string;
  participant_ids?: string[];
  message_count?: number;
}

export interface Agent {
  id: string;
  fqid?: string;
  network_id?: string;
  rooms?: string[];
}

export interface Pairing {
  id: string;
  remote_network_id: string;
  remote_network_name?: string;
  remote_base_url?: string;
  status?: string;
}

export interface MessagePart {
  kind: "text" | "url" | "data" | "file";
  text?: string;
  url?: string;
  data?: unknown;
  filename?: string;
  media_type?: string;
}

export interface MessageTarget {
  kind: "room" | "dm";
  room_id?: string;
  dm_id?: string;
  participant_ids?: string[];
}

export interface MessageFrom {
  type: "human" | "agent" | string;
  id: string;
  name?: string;
}

export interface Message {
  id?: string;
  network_id?: string;
  from: MessageFrom;
  target: MessageTarget;
  parts: MessagePart[];
  mentions?: string[];
  created_at: string;
}

export interface MessagePage {
  messages: Message[];
  page?: { has_more?: boolean; next_before?: string };
}

export interface MoltnetEvent {
  type: string;
  created_at: string;
  message?: Message;
}

export type SelectionKind = "room" | "dm" | "events" | "agent" | "pairing";

export type Selection =
  | { kind: "room"; id: string }
  | { kind: "dm"; id: string }
  | { kind: "agent"; id: string }
  | { kind: "pairing"; id: string }
  | { kind: "events" };

export type MessageTargetSelection =
  | { kind: "room"; id: string }
  | { kind: "dm"; id: string };

export function isMessageTargetSelection(
  selection: Selection | null,
): selection is MessageTargetSelection {
  return (
    !!selection && (selection.kind === "room" || selection.kind === "dm")
  );
}
