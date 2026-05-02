import { useRooms } from "../../hooks/useRooms";
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
                  trailing={
                    (room.members ?? []).length > 0
                      ? `${room.members!.length} members`
                      : undefined
                  }
                />
              );
            })}
          </div>
        )}
      </Panel.Body>
    </Panel>
  );
}
