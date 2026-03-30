export function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

export function textFromParts(parts = []) {
  return parts
    .map((part) => {
      if (part.kind === "text") return part.text || "";
      if (part.kind === "url") return part.url || "";
      if (part.kind === "data") return JSON.stringify(part.data || {});
      return part.filename || part.media_type || "";
    })
    .filter(Boolean)
    .join("\n");
}

export function targetKey(kind, id) {
  return `${kind}:${id}`;
}

export function summarizeTarget(target) {
  if (target.kind === "room") {
    return `Room ${target.id}`;
  }
  if (target.kind === "dm") {
    return `Direct channel ${target.id}`;
  }
  return target.id;
}

export function renderSelectableList(container, items, renderItem, currentKey) {
  container.innerHTML = "";
  if (!items.length) {
    container.innerHTML = `<div class="empty">Nothing connected yet.</div>`;
    return;
  }

  for (const item of items) {
    const button = document.createElement("button");
    button.className = "item";
    const key = renderItem.key(item);
    button.dataset.key = key;
    button.classList.toggle("active", key === currentKey);
    button.innerHTML = renderItem.html(item);
    button.onclick = () => renderItem.onSelect(item);
    container.appendChild(button);
  }
}

export function renderDetailRows(rows) {
  return rows
    .map(
      (row) => `
        <div class="detail-row">
          <span class="detail-label">${escapeHTML(row.label)}</span>
          <div>${escapeHTML(row.value)}</div>
        </div>
      `,
    )
    .join("");
}

export function renderRoomDetails(room) {
  return renderDetailRows([
    { label: "Room", value: room.name || room.id },
    { label: "Canonical ID", value: room.fqid || room.id },
    { label: "Members", value: (room.members || []).join(", ") || "None" },
  ]);
}

export function renderDMDetails(dm) {
  return renderDetailRows([
    { label: "Direct Channel", value: dm.id },
    { label: "Canonical ID", value: dm.fqid || dm.id },
    { label: "Participants", value: (dm.participant_ids || []).join(", ") || "None" },
    { label: "Messages", value: String(dm.message_count || 0) },
  ]);
}

export function renderAgentDetails(agent) {
  return renderDetailRows([
    { label: "Agent", value: agent.id },
    { label: "Canonical ID", value: agent.fqid || agent.id },
    { label: "Network", value: agent.network_id || "unknown" },
    { label: "Rooms", value: (agent.rooms || []).join(", ") || "None" },
  ]);
}

export function renderPairingDetails(pairing) {
  return renderDetailRows([
    { label: "Pairing", value: pairing.id },
    { label: "Remote Network", value: pairing.remote_network_name || pairing.remote_network_id },
    { label: "Remote ID", value: pairing.remote_network_id },
    { label: "Status", value: pairing.status || "unknown" },
  ]);
}

export function renderMessage(message) {
  const tags = [];
  if (message.network_id) {
    tags.push(`<span class="message-tag">${escapeHTML(message.network_id)}</span>`);
  }
  if (message.target?.kind === "room" && message.target.room_id) {
    tags.push(`<span class="message-tag">#${escapeHTML(message.target.room_id)}</span>`);
  }
  if (message.target?.kind === "dm" && message.target.dm_id) {
    tags.push(`<span class="message-tag">${escapeHTML(message.target.dm_id)}</span>`);
  }
  for (const mention of message.mentions || []) {
    tags.push(`<span class="message-tag">@${escapeHTML(mention)}</span>`);
  }

  return `
    <article class="message">
      <div class="message-head">
        <strong>${escapeHTML(message.from?.name || message.from?.id || "unknown")}</strong>
        <span class="muted">${new Date(message.created_at).toLocaleString()}</span>
      </div>
      <div class="message-body">${escapeHTML(textFromParts(message.parts) || "(non-text message)")}</div>
      <div class="message-meta">${tags.join("")}</div>
    </article>
  `;
}
