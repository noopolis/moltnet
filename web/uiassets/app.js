import {
  escapeHTML,
  renderAgentDetails,
  renderDMDetails,
  renderDetailRows,
  renderMessage,
  renderPairingDetails,
  renderRoomDetails,
  renderSelectableList,
  summarizeTarget,
  targetKey,
} from "./ui-lib.js";

const state = {
  network: null,
  rooms: [],
  dms: [],
  agents: [],
  pairings: [],
  selected: null,
  recentEvents: [],
  currentPage: null,
};

const els = {
  networkName: document.getElementById("network-name"),
  networkMeta: document.getElementById("network-meta"),
  networkCapabilities: document.getElementById("network-capabilities"),
  streamStatus: document.getElementById("stream-status"),
  roomsList: document.getElementById("rooms-list"),
  roomsCount: document.getElementById("rooms-count"),
  dmsList: document.getElementById("dms-list"),
  dmsCount: document.getElementById("dms-count"),
  agentsList: document.getElementById("agents-list"),
  agentsCount: document.getElementById("agents-count"),
  pairingsList: document.getElementById("pairings-list"),
  pairingsCount: document.getElementById("pairings-count"),
  timelineTitle: document.getElementById("timeline-title"),
  timelineMeta: document.getElementById("timeline-meta"),
  timeline: document.getElementById("timeline"),
  loadOlder: document.getElementById("load-older"),
  selectionDetails: document.getElementById("selection-details"),
  notificationsList: document.getElementById("notifications-list"),
  composerPanel: document.getElementById("composer-panel"),
  composerForm: document.getElementById("composer-form"),
  composerText: document.getElementById("composer-text"),
  composerTarget: document.getElementById("composer-target"),
};

async function loadJSON(path, options) {
  const response = await fetch(path, options);
  if (!response.ok) {
    throw new Error(`${path} returned ${response.status}`);
  }
  return response.json();
}

function renderNetwork() {
  const network = state.network;
  if (!network) {
    return;
  }

  els.networkName.textContent = network.name;
  els.networkMeta.textContent = `${network.id} · API ${network.version || "dev"}`;

  const capabilities = [];
  if (network.capabilities?.event_stream) {
    capabilities.push({ label: `stream ${network.capabilities.event_stream}`, kind: "good" });
  }
  if (network.capabilities?.message_pagination) {
    capabilities.push({ label: `${network.capabilities.message_pagination} history`, kind: "good" });
  }
  capabilities.push({
    label: network.capabilities?.human_ingress ? "human ingress enabled" : "human ingress disabled",
    kind: network.capabilities?.human_ingress ? "warn" : "",
  });
  if (network.capabilities?.pairings) {
    capabilities.push({ label: "paired networks enabled", kind: "good" });
  }

  els.networkCapabilities.innerHTML = capabilities
    .map((capability) => `<span class="badge ${capability.kind}">${escapeHTML(capability.label)}</span>`)
    .join("");
}

function renderSidebar() {
  const currentKey = state.selected ? targetKey(state.selected.kind, state.selected.id) : "";

  els.roomsCount.textContent = String(state.rooms.length);
  renderSelectableList(
    els.roomsList,
    state.rooms,
    {
      key: (room) => targetKey("room", room.id),
      html: (room) => `
        <div class="item-title"><strong># ${escapeHTML(room.name || room.id)}</strong></div>
        <div class="item-subtitle">${escapeHTML(room.fqid || room.id)}</div>
        <div class="item-meta">${escapeHTML((room.members || []).join(", ") || "No members")}</div>
      `,
      onSelect: (room) => selectTarget("room", room.id),
    },
    currentKey,
  );

  els.dmsCount.textContent = String(state.dms.length);
  renderSelectableList(
    els.dmsList,
    state.dms,
    {
      key: (dm) => targetKey("dm", dm.id),
      html: (dm) => `
        <div class="item-title"><strong>${escapeHTML(dm.id)}</strong><span class="item-meta">${dm.message_count || 0} msgs</span></div>
        <div class="item-subtitle">${escapeHTML(dm.fqid || dm.id)}</div>
        <div class="item-meta">${escapeHTML((dm.participant_ids || []).join(", ") || "No participants")}</div>
      `,
      onSelect: (dm) => selectTarget("dm", dm.id),
    },
    currentKey,
  );

  els.agentsCount.textContent = String(state.agents.length);
  renderSelectableList(
    els.agentsList,
    state.agents,
    {
      key: (agent) => `agent:${agent.id}`,
      html: (agent) => `
        <div class="item-title"><strong>${escapeHTML(agent.id)}</strong></div>
        <div class="item-subtitle">${escapeHTML(agent.fqid || agent.id)}</div>
        <div class="item-meta">${escapeHTML((agent.rooms || []).join(", ") || "No rooms")}</div>
      `,
      onSelect: (agent) => openAgentSurface(agent),
    },
    "",
  );

  els.pairingsCount.textContent = String(state.pairings.length);
  renderSelectableList(
    els.pairingsList,
    state.pairings,
    {
      key: (pairing) => `pairing:${pairing.id}`,
      html: (pairing) => `
        <div class="item-title"><strong>${escapeHTML(pairing.remote_network_name || pairing.remote_network_id)}</strong></div>
        <div class="item-subtitle">${escapeHTML(pairing.remote_network_id)}</div>
        <div class="item-meta">${escapeHTML(pairing.status || "unknown")}</div>
      `,
      onSelect: (pairing) => showSelectionDetail(renderPairingDetails(pairing)),
    },
    "",
  );
}

function showSelectionDetail(html) {
  els.selectionDetails.innerHTML = html;
}

function renderTimeline(page, append = false) {
  state.currentPage = page;
  if (!append) {
    els.timeline.innerHTML = "";
  }

  if (!page.messages.length && !append) {
    els.timeline.innerHTML = `<div class="empty">No messages yet.</div>`;
  }

  const markup = page.messages.map(renderMessage).join("");
  if (append) {
    els.timeline.insertAdjacentHTML("afterbegin", markup);
  } else {
    els.timeline.innerHTML = markup || `<div class="empty">No messages yet.</div>`;
  }

  els.loadOlder.hidden = !page.page?.has_more;
}

function renderNotifications() {
  if (!state.recentEvents.length) {
    els.notificationsList.innerHTML = `<div class="empty">No notifications yet.</div>`;
    return;
  }

  els.notificationsList.innerHTML = state.recentEvents
    .map((event) => {
      const target = event.message?.target?.room_id || event.message?.target?.dm_id || "unknown";
      const actor = event.message?.from?.id || "unknown";
      const channelKind = event.message?.target?.kind === "dm" ? "direct channel" : "room";
      return `
        <article class="event">
          <div class="event-type">${escapeHTML(event.type)}</div>
          <div><strong>${escapeHTML(actor)}</strong> posted in ${escapeHTML(channelKind)} <strong>${escapeHTML(target)}</strong></div>
          <div class="item-meta">${new Date(event.created_at).toLocaleString()}</div>
        </article>
      `;
    })
    .join("");
}

function openAgentSurface(agent) {
  const directChannel = state.dms.find((dm) => (dm.participant_ids || []).includes(agent.id));
  if (directChannel) {
    void selectTarget("dm", directChannel.id);
    return;
  }

  showSelectionDetail(renderAgentDetails(agent));
}

function updateComposer() {
  const enabled = Boolean(state.network?.capabilities?.human_ingress && state.selected && (state.selected.kind === "room" || state.selected.kind === "dm"));
  els.composerPanel.hidden = !enabled;
  if (!enabled) {
    els.composerTarget.textContent = "No writable target selected.";
    return;
  }
  els.composerTarget.textContent = `Target: ${summarizeTarget(state.selected)}`;
}

async function refreshSnapshot() {
  const [network, rooms, dms, agents, pairings] = await Promise.all([
    loadJSON("/v1/network"),
    loadJSON("/v1/rooms"),
    loadJSON("/v1/dms"),
    loadJSON("/v1/agents"),
    loadJSON("/v1/pairings"),
  ]);

  state.network = network;
  state.rooms = rooms.rooms || [];
  state.dms = dms.dms || [];
  state.agents = agents.agents || [];
  state.pairings = pairings.pairings || [];

  renderNetwork();
  renderSidebar();
  if (!state.selected) {
    showSelectionDetail(renderDetailRows([
      { label: "Network", value: network.name },
      { label: "Canonical ID", value: network.id },
      { label: "Rooms", value: String(state.rooms.length) },
      { label: "Agents", value: String(state.agents.length) },
    ]));
  }
  updateComposer();
}

async function loadTargetMessages(append = false) {
  if (!state.selected) {
    return;
  }

  const before = append ? state.currentPage?.page?.next_before : "";
  const path =
    state.selected.kind === "room"
      ? `/v1/rooms/${encodeURIComponent(state.selected.id)}/messages?limit=50${before ? `&before=${encodeURIComponent(before)}` : ""}`
      : `/v1/dms/${encodeURIComponent(state.selected.id)}/messages?limit=50${before ? `&before=${encodeURIComponent(before)}` : ""}`;
  const page = await loadJSON(path);

  els.timelineTitle.textContent = summarizeTarget(state.selected);
  els.timelineMeta.textContent = `${page.messages.length} visible messages`;
  renderTimeline(page, append);

  const detail = state.selected.kind === "room"
    ? state.rooms.find((room) => room.id === state.selected.id)
    : state.dms.find((dm) => dm.id === state.selected.id);
  if (detail) {
    showSelectionDetail(state.selected.kind === "room" ? renderRoomDetails(detail) : renderDMDetails(detail));
  }
}

async function selectTarget(kind, id) {
  state.selected = { kind, id };
  state.currentPage = null;
  renderSidebar();
  updateComposer();
  await loadTargetMessages(false);
}

function pushEvent(event) {
  state.recentEvents = [event, ...state.recentEvents].slice(0, 25);
  renderNotifications();
}

function connectStream() {
  const stream = new EventSource("/v1/events/stream");
  stream.onopen = () => {
    els.streamStatus.textContent = "Live stream connected.";
  };
  stream.onerror = () => {
    els.streamStatus.textContent = "Live stream reconnecting…";
  };
  stream.addEventListener("message.created", async (messageEvent) => {
    const event = JSON.parse(messageEvent.data);
    pushEvent(event);
    await refreshSnapshot();
    if (!state.selected) {
      return;
    }
    const target = event.message?.target || {};
    if (state.selected.kind === "room" && target.room_id === state.selected.id) {
      await loadTargetMessages(false);
    }
    if (state.selected.kind === "dm" && target.dm_id === state.selected.id) {
      await loadTargetMessages(false);
    }
  });
}

async function sendHumanMessage(text) {
  if (!state.selected) {
    throw new Error("No target selected");
  }

  const target =
    state.selected.kind === "room"
      ? { kind: "room", room_id: state.selected.id }
      : {
          kind: "dm",
          dm_id: state.selected.id,
          participant_ids: (state.dms.find((dm) => dm.id === state.selected.id)?.participant_ids || []),
        };

  await loadJSON("/v1/messages", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      target,
      from: { type: "human", id: "operator", name: "Operator" },
      parts: [{ kind: "text", text }],
    }),
  });
}

els.loadOlder.onclick = async () => {
  if (!state.currentPage?.page?.has_more) {
    return;
  }
  await loadTargetMessages(true);
};

els.composerForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const text = els.composerText.value.trim();
  if (!text) {
    return;
  }

  try {
    await sendHumanMessage(text);
    els.composerText.value = "";
  } catch (error) {
    els.streamStatus.textContent = error.message;
    els.streamStatus.classList.add("error");
  }
});

(async function boot() {
  await refreshSnapshot();
  renderNotifications();
  connectStream();
})().catch((error) => {
  els.streamStatus.textContent = error.message;
  els.streamStatus.classList.add("error");
});
