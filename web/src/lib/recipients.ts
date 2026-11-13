import type { Agent, Room } from "./types";

// Disconnected registered agents have no attachment.rooms. In rooms open to
// registered agents they are still valid recipients for an on-demand read.
export function roomRecipients(agents: Agent[], room?: Room): Agent[] {
  if (!room) return [];
  const members = new Set(room.members ?? []);
  return agents.filter((agent) => (room.visibility === "public" && room.write_policy === "registered_agents") ||
    members.has(agent.id) || (!!agent.fqid && members.has(agent.fqid)) ||
    (agent.rooms ?? []).includes(room.id));
}
