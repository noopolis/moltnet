import { useMemo } from "react";
import { usePairingAgents } from "../../../hooks/usePairingAgents";
import { usePairingNetwork } from "../../../hooks/usePairingNetwork";
import { usePairingRooms } from "../../../hooks/usePairingRooms";
import { usePairings } from "../../../hooks/usePairings";
import { useSelection } from "../../../providers";
import { DetailRow } from "../../DetailRow";
import { ListItem } from "../../ListItem";
import { Panel } from "../../Panel";

export function PairingView() {
  const { selected } = useSelection();
  const { data: pairings = [] } = usePairings();
  const pairingId = selected?.kind === "pairing" ? selected.id : null;

  const pairing = useMemo(
    () => (pairingId ? pairings.find((p) => p.id === pairingId) : null),
    [pairings, pairingId],
  );

  const networkQuery = usePairingNetwork(pairingId);
  const roomsQuery = usePairingRooms(pairingId);
  const agentsQuery = usePairingAgents(pairingId);

  if (!pairing || !pairingId) {
    return (
      <Panel>
        <Panel.Header>
          <Panel.Title>PAIRING</Panel.Title>
        </Panel.Header>
        <Panel.Body>
          <p className="text-faint text-xs px-2 py-1.5">
            pairing not found{pairingId ? ` — ${pairingId}` : ""}.
          </p>
        </Panel.Body>
      </Panel>
    );
  }

  const remoteName =
    pairing.remote_network_name || pairing.remote_network_id || "—";
  const status = pairing.status || "unknown";
  const remoteNetwork = networkQuery.data;
  const remoteRooms = roomsQuery.data ?? [];
  const remoteAgents = agentsQuery.data ?? [];
  const remoteUnreachable =
    !!networkQuery.error || !!roomsQuery.error || !!agentsQuery.error;

  return (
    <Panel>
      <Panel.Header>
        <Panel.Title>{`PAIRING: ${pairing.id}`}</Panel.Title>
        <Panel.Count>{status}</Panel.Count>
      </Panel.Header>
      <Panel.Body>
        <div className="grid gap-2 text-xs mb-4">
          <DetailRow label="pairing" value={pairing.id} />
          <DetailRow label="remote network" value={remoteName} />
          <DetailRow label="remote id" value={pairing.remote_network_id} />
          {pairing.remote_base_url ? (
            <DetailRow label="remote url" value={pairing.remote_base_url} />
          ) : null}
          <DetailRow label="status" value={status} />
        </div>

        <SectionHeader title="REMOTE NETWORK" />
        {networkQuery.isLoading ? (
          <p className="text-faint text-xs px-2 py-1 mb-4">loading…</p>
        ) : networkQuery.error ? (
          <p className="text-coral text-xs px-2 py-1 mb-4">
            unreachable — {(networkQuery.error as Error).message}
          </p>
        ) : remoteNetwork ? (
          <div className="grid gap-2 text-xs mb-4">
            <DetailRow
              label="name"
              value={remoteNetwork.name || remoteNetwork.id}
            />
            <DetailRow label="id" value={remoteNetwork.id} />
            <DetailRow label="version" value={remoteNetwork.version || "dev"} />
            <DetailRow
              label="event stream"
              value={remoteNetwork.capabilities?.event_stream || "—"}
            />
            <DetailRow
              label="human ingress"
              value={
                remoteNetwork.capabilities?.human_ingress ? "enabled" : "disabled"
              }
            />
          </div>
        ) : (
          <p className="text-faint text-xs px-2 py-1 mb-4">no data.</p>
        )}

        <SectionHeader title="REMOTE ROOMS" count={remoteRooms.length} />
        <RemoteList
          loading={roomsQuery.isLoading}
          error={roomsQuery.error}
          items={remoteRooms.map((room) => ({
            key: room.id,
            title: <># {room.name || room.id}</>,
            trailing:
              (room.members ?? []).length > 0
                ? `${room.members!.length} members`
                : undefined,
          }))}
          emptyText="no rooms exposed by the remote."
        />

        <SectionHeader title="REMOTE AGENTS" count={remoteAgents.length} />
        <RemoteList
          loading={agentsQuery.isLoading}
          error={agentsQuery.error}
          items={remoteAgents.map((agent) => ({
            key: agent.id,
            title: agent.id,
            trailing:
              (agent.rooms ?? []).length > 0
                ? `${agent.rooms!.length} rooms`
                : undefined,
          }))}
          emptyText="no agents exposed by the remote."
        />

        {remoteUnreachable ? (
          <p className="text-faint text-[11px] px-2 py-2 mt-2 border-t border-dashed border-white/[0.05]">
            note: this pairing's remote URL did not respond. data above may be
            stale or empty.
          </p>
        ) : null}
      </Panel.Body>
    </Panel>
  );
}

function SectionHeader({
  title,
  count,
}: {
  title: string;
  count?: number;
}) {
  return (
    <div className="text-[11px] tracking-[0.12em] text-green mt-1 mb-1.5">
      [ {title} ]
      {count !== undefined ? <span> ( {count} )</span> : null}
    </div>
  );
}

interface RemoteListItem {
  key: string;
  title: React.ReactNode;
  trailing?: React.ReactNode;
}

function RemoteList({
  loading,
  error,
  items,
  emptyText,
}: {
  loading: boolean;
  error: unknown;
  items: RemoteListItem[];
  emptyText: string;
}) {
  if (loading) {
    return <p className="text-faint text-xs px-2 py-1 mb-4">loading…</p>;
  }
  if (error) {
    return (
      <p className="text-coral text-xs px-2 py-1 mb-4">
        unreachable — {(error as Error).message}
      </p>
    );
  }
  if (items.length === 0) {
    return <p className="text-faint text-xs px-2 py-1 mb-4">{emptyText}</p>;
  }
  return (
    <div className="flex flex-col mb-4">
      {items.map((item) => (
        <ListItem
          key={item.key}
          showMarker={false}
          title={item.title}
          trailing={item.trailing}
        />
      ))}
    </div>
  );
}
