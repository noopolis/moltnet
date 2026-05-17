import { useRooms } from "../../hooks/useRooms";
import type { Room } from "../../lib/types";
import { useSelection } from "../../providers";
import { ListItem } from "../ListItem";
import { Panel } from "../Panel";

export function RoomsPanel() {
  const { data: rooms = [] } = useRooms();
  const { selected, select } = useSelection();

  return (
    <Panel>
      <Panel.Header>
        <Panel.Title>ROOMS</Panel.Title>
        <Panel.Count>{rooms.length}</Panel.Count>
      </Panel.Header>
      <Panel.Body>
        {rooms.length === 0 ? (
          <p className="text-faint text-xs px-2 py-1.5">no rooms connected.</p>
        ) : (
          <div className="flex flex-col">
            {rooms.map((room) => {
              const active = selected?.kind === "room" && selected.id === room.id;
              return (
                <ListItem
                  key={room.id}
                  active={active}
                  onClick={() => select({ kind: "room", id: room.id })}
                  title={<># {room.name || room.id}</>}
                  subtitle={roomPolicyLabel(room)}
                  trailing={roomAccessLabel(room)}
                />
              );
            })}
          </div>
        )}
      </Panel.Body>
    </Panel>
  );
}

function roomPolicyLabel(room: Room): string {
  const visibility = room.visibility === "public" ? "public read" : "private read";
  const writePolicy = room.write_policy ?? "members";
  switch (writePolicy) {
    case "registered_agents":
      return `${visibility} / registered agents write`;
    case "operators":
      return `${visibility} / operators write`;
    default:
      return `${visibility} / members write`;
  }
}

function roomAccessLabel(room: Room): string | undefined {
  if (room.access?.can_write === true) {
    return "write";
  }
  if (room.access?.can_read === true) {
    return "read";
  }
  const members = room.members?.length ?? 0;
  return members > 0 ? `${members} members` : undefined;
}
