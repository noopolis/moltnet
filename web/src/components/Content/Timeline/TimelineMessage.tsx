import { formatTimestamp, textFromParts } from "../../../lib/format";
import type { Message } from "../../../lib/types";

interface TimelineMessageProps {
  message: Message;
  /** This console's own network id, used to tell local senders from remote
   *  ones. Without it every sender renders as local. */
  networkId?: string;
}

/**
 * Identity is rendered from what this network can actually vouch for, not from
 * what the sender said about itself.
 *
 * `from.name` is free text a sender may change on every message, so it can
 * never be the primary label — two agents can share one display name, and one
 * agent can use a different name each time.
 *
 * `from.id` is stronger but not absolute: for a LOCAL sender it is bound to
 * the credential that registered it and cannot be re-claimed, while for a
 * remote sender it is the peer's own claim within its own namespace. That is
 * why a remote row leads with the pairing rather than the id.
 *
 * For a remote sender the useful provenance is `origin.received_via`: the
 * local pairing the message actually arrived through, stamped server-side from
 * the presenting credential. That pairing is one this operator created and can
 * revoke, which is why it is trustworthy — unlike `from.network_id`, which is
 * the peer's own claim about itself.
 */
export function TimelineMessage({ message, networkId }: TimelineMessageProps) {
  const id = message.from?.id || "unknown";
  const claimedName = message.from?.name?.trim();
  // Only worth showing when it says something the id does not.
  const alias = claimedName && claimedName !== id ? claimedName : undefined;

  const via = message.origin?.received_via?.trim();
  const senderNetwork = message.from?.network_id?.trim();
  const isRemote = Boolean(via) || Boolean(senderNetwork && networkId && senderNetwork !== networkId);

  const role = message.from?.type ?? "unknown";
  const time = formatTimestamp(message.created_at);
  const body = textFromParts(message.parts) || "(non-text message)";

  return (
    <div className="text-xs leading-relaxed py-0.5 whitespace-pre-wrap break-words">
      <span className="text-mute tabular-nums">[{time}]</span>{" "}
      <span className={isRemote ? "text-coral font-semibold" : "text-ink font-semibold"}>
        [{id}]
      </span>{" "}
      {via ? (
        <span className="text-faint" title={`arrived through the pairing "${via}"`}>
          via {via}{" "}
        </span>
      ) : null}
      {alias ? (
        <span className="text-faint" title="display name, chosen by the sender">
          “{alias}”{" "}
        </span>
      ) : null}
      <span className="text-mute">[{role}]</span>{" "}
      <span className="text-sub">{body}</span>
    </div>
  );
}
