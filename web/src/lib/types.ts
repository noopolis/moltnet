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
  public_read?: boolean;
  event_stream?: string;
  message_pagination?: string;
  human_ingress?: boolean;
  direct_messages?: boolean;
  debug_events?: boolean;
  pairings?: boolean;
  attachment_protocol?: string;
}

export interface NetworkConsole {
  can_manage_automation?: boolean;
  can_follow?: boolean;
  can_send_human?: boolean;
  viewer?: { kind: "anonymous" | "token" | "agent"; id?: string };
  can_read_pairings?: boolean;
  can_read_dms?: boolean;
  can_connect_agent?: boolean;
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
  follow?: { id: string; remote_base_url: string; remote_room_id: string };
  last_message_at?: string;
  id: string;
  name: string;
  fqid?: string;
  members?: string[];
  visibility?: "public" | "private" | string;
  write_policy?: "members" | "registered_agents" | "operators" | string;
  access?: RoomAccess;
}

export interface RoomAccess {
  can_read?: boolean;
  can_write?: boolean;
  reason?: string;
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
  delivery?: PairingDeliveryStatus;
}

export interface PairingDeliveryStatus {
  rejected?: number;
  last_rejection_reason?: string;
  pending: number;
  oldest_pending_age_seconds: number;
  last_error?: string;
  dropped: number;
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
  kind: "text" | "url" | "data" | "file" | "image" | "audio";
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
  /** Self-asserted display name. Free text, changeable per message; never
   *  use it alone to identify a sender. */
  name?: string;
  /** The network this actor belongs to. Peer-supplied on a relayed message. */
  network_id?: string;
  fqid?: string;
  /** Server-stamped: the sender proved this identity with a credential.
   *  Always false for remote actors; they prove themselves to their own
   *  network, not to this one. */
  credential_bound?: boolean;
}

export interface MessageOrigin {
  /** Untrusted testimony supplied by an anonymous follow origin. */
  claimed_author?: MessageFrom;
  claimed_network_id?: string;
  claimed_message_id?: string;
  claimed_discovery_network_id?: string;
  network_id?: string;
  message_id?: string;
  /** Server-stamped: the local pairing this message actually arrived
   *  through. The only sender-provenance on a relayed message that this
   *  operator authored; everything else is the peer's own claim. Empty for
   *  locally-originated messages. */
  received_via?: string;
  /** Server-stamped verdict on the message's signed envelope. Absent on
   *  live stream events and on networks that predate shared rooms. */
  signature?: MessageSignature;
  /** Signing key fingerprint, `sha256:<64 hex>`. */
  fingerprint?: string;
  /** For `introduced`: the room home network that vouched for the key. */
  introduced_by?: string;
}

export type MessageSignature =
  | "verified"
  | "introduced"
  | "unconfirmed"
  | "invalid"
  | "unsigned";

export interface Message {
  id?: string;
  network_id?: string;
  origin?: MessageOrigin;
  from: MessageFrom;
  target: MessageTarget;
  parts: MessagePart[];
  mentions?: string[];
  /** Mentions this network resolved locally (signed messages only). */
  local_mentions?: string[];
  /** For a signed message: the origin's signed creation time. */
  created_at: string;
  /** When this network received the message. */
  received_at?: string;
  /** Shared rooms: the home's placement order; display sorts by it. */
  room_seq?: number;
  /** Shared rooms: the membership epoch the message was placed under. */
  membership_epoch?: number;
}

/** Trust this network holds in a shared-room member's key. */
export type HubTrust = "verified" | "introduced" | "unconfirmed" | "self" | "conflict";

export interface RoomHubMember {
  network_id: string;
  role?: string;
  fingerprint?: string;
  trust?: HubTrust | string;
}

export type RoomHubEventKind =
  | "genesis"
  | "add"
  | "remove"
  | "leave"
  | "set_role"
  | "set_policy";

export interface RoomHubEvent {
  seq: number;
  epoch?: number;
  kind: RoomHubEventKind | string;
  network_id?: string;
  role?: string;
  history?: string;
  created_at: string;
}

/** `GET /v1/rooms/{id}/hub`: the membership log of a shared room. */
export interface RoomHub {
  global_room_id: string;
  local_room_id?: string;
  home_network_id: string;
  is_home?: boolean;
  membership_epoch?: number;
  history?: string;
  members?: RoomHubMember[];
  events?: RoomHubEvent[];
  /** Placed messages this member may see but the home withheld from catch-up. */
  withheld?: RoomHubWithheld[];
}

export interface RoomHubWithheld {
  room_seq: number;
  membership_epoch?: number;
  reason: string;
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
