export interface NetworkProtocols {
  http?: string[];
  attach?: string[];
  pair?: string[];
}

export type NetworkWarningSeverity = "info" | "warning" | "error" | string;

export interface NetworkWarning {
  severity: NetworkWarningSeverity;
  code: string;
  message: string;
  action?: string;
  docs_url?: string;
}

export interface NetworkCapabilities {
  event_stream?: string;
  message_pagination?: string;
  human_ingress?: boolean;
  direct_messages?: boolean;
  debug_events?: boolean;
  pairings?: boolean;
  attachment_protocol?: string;
}

export interface NetworkConsole {
  can_send_human?: boolean;
}

export interface Network {
  id: string;
  name: string;
  version: string;
  protocols?: NetworkProtocols;
  capabilities: NetworkCapabilities;
  console?: NetworkConsole;
  warnings?: NetworkWarning[];
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

export interface Thread {
  id: string;
  room_id?: string;
  root_message_id?: string;
  message_count?: number;
}

export interface Agent {
  id: string;
  name?: string;
  fqid?: string;
  network_id?: string;
  rooms?: string[];
  connected?: boolean;
}

export interface Pairing {
  id: string;
  remote_network_id: string;
  remote_network_name?: string;
  remote_base_url?: string;
  status?: string;
  diagnostics?: PairingDiagnostics;
}

export interface PairingDiagnostics {
  checked_at?: string;
  remote_version?: string;
  remote_network_id?: string;
  remote_protocols?: NetworkProtocols;
  reason?: string;
  message?: string;
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
  agent?: {
    agent_id: string;
    network_id?: string;
    fqid?: string;
    name?: string;
    message_id?: string;
    reason?: string;
    target?: MessageTarget;
    error?: string;
  };
  message?: Message;
  room?: Room;
  thread?: Thread;
  dm?: DirectChannel;
  pairing?: Pairing;
  replay_gap?: {
    requested_event_id?: string;
    oldest_event_id?: string;
    newest_event_id?: string;
  };
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
