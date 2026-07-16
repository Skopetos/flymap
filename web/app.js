// Air Traffic Tracker frontend. No build step, no framework — just Leaflet
// (loaded globally from the CDN script tag) plus this ES module.

const ATHENS = { lat: 37.98, lon: 23.72 };
const POLL_INTERVAL_MS = 5000;
const MISSED_POLL_LIMIT = 3;
const GLIDE_MS = 4500;
const ICON_SIZE = 22;

const PLANE_PATH =
  "M12 1 L16 9 L23 12.5 L16 14.5 L17 23 L12 19 L7 23 L8 14.5 L1 12.5 L8 9 Z";

let center = { ...ATHENS };
let radiusNm = 100;
let pollTimer = null;
let selectedHex = null;

// hex -> { marker, missed, data }
const aircraft = new Map();

const countEl = document.getElementById("aircraft-count");
const staleEl = document.getElementById("stale-indicator");
const radiusSelect = document.getElementById("radius-select");
const locateBtn = document.getElementById("locate-btn");
const detailPanel = document.getElementById("detail-panel");
const detailContent = document.getElementById("detail-content");
const detailClose = document.getElementById("detail-close");

const map = L.map("map", { zoomControl: false, attributionControl: true }).setView(
  [center.lat, center.lon],
  8
);
L.control.zoom({ position: "bottomright" }).addTo(map);
L.tileLayer(
  "https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png",
  {
    attribution:
      '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>',
    subdomains: "abcd",
    maxZoom: 19,
  }
).addTo(map);

function planeIcon(track) {
  return L.divIcon({
    className: "aircraft-icon",
    html: `<svg viewBox="0 0 24 24" width="${ICON_SIZE}" height="${ICON_SIZE}" style="transform:rotate(${track || 0}deg)"><path d="${PLANE_PATH}"/></svg>`,
    iconSize: [ICON_SIZE, ICON_SIZE],
    iconAnchor: [ICON_SIZE / 2, ICON_SIZE / 2],
  });
}

function selectMarker(hex) {
  if (selectedHex && aircraft.has(selectedHex)) {
    aircraft.get(selectedHex).marker.getElement()?.classList.remove("selected");
  }
  selectedHex = hex;
  if (hex && aircraft.has(hex)) {
    aircraft.get(hex).marker.getElement()?.classList.add("selected");
  }
}

function fmtAlt(altBaro) {
  if (altBaro === "ground" || altBaro === undefined || altBaro === null) return "GND";
  return `${altBaro.toLocaleString()} ft`;
}

function updateDetailStats(hex) {
  const entry = aircraft.get(hex);
  if (!entry) return;
  const d = entry.data;
  const values = detailContent.querySelectorAll(".detail-row .value");
  if (values.length < 3) return;
  values[0].textContent = fmtAlt(d.alt_baro);
  values[1].textContent = d.gs ? `${Math.round(d.gs)} kt` : "—";
  values[2].innerHTML = d.track !== undefined ? `${Math.round(d.track)}&deg;` : "—";
}

function openDetail(hex) {
  const entry = aircraft.get(hex);
  if (!entry) return;
  selectMarker(hex);

  const d = entry.data;
  const callsign = d.flight || "Unknown";

  detailContent.innerHTML = `
    <h2>${callsign}</h2>
    <div class="subtitle">${d.t || "Unknown type"}${d.r ? " &middot; " + d.r : ""}</div>
    <div class="detail-row"><span class="label">Altitude</span><span class="value">${fmtAlt(d.alt_baro)}</span></div>
    <div class="detail-row"><span class="label">Ground speed</span><span class="value">${d.gs ? Math.round(d.gs) + " kt" : "—"}</span></div>
    <div class="detail-row"><span class="label">Heading</span><span class="value">${d.track !== undefined ? Math.round(d.track) + "&deg;" : "—"}</span></div>
    <div class="route-block" id="route-block">
      <div class="route-unknown">Loading route&hellip;</div>
    </div>
  `;
  detailPanel.classList.add("open");

  if (!d.flight) {
    document.getElementById("route-block").innerHTML =
      '<div class="route-unknown">Route unknown</div>';
    return;
  }

  fetch(`/api/route/${encodeURIComponent(d.flight)}`)
    .then((r) => r.json())
    .then((route) => {
      const block = document.getElementById("route-block");
      if (!block) return; // panel closed/changed before response arrived
      if (!route.known) {
        block.innerHTML = '<div class="route-unknown">Route unknown</div>';
        return;
      }
      block.innerHTML = `
        <div class="airline">${route.airline || "Unknown airline"}</div>
        <div class="route-path">
          <div class="airport">
            <div class="code">${route.origin?.iata || route.origin?.icao || "?"}</div>
            <div class="name">${route.origin?.name || ""}</div>
          </div>
          <div class="arrow">&#8594;</div>
          <div class="airport">
            <div class="code">${route.destination?.iata || route.destination?.icao || "?"}</div>
            <div class="name">${route.destination?.name || ""}</div>
          </div>
        </div>
      `;
    })
    .catch(() => {
      const block = document.getElementById("route-block");
      if (block) block.innerHTML = '<div class="route-unknown">Route unknown</div>';
    });
}

function closeDetail() {
  detailPanel.classList.remove("open");
  selectMarker(null);
}

detailClose.addEventListener("click", closeDetail);

function upsertAircraft(a) {
  const existing = aircraft.get(a.hex);

  if (existing) {
    existing.missed = 0;
    existing.data = a;
    const icon = existing.marker.getElement();
    if (icon) icon.style.transition = `transform ${GLIDE_MS}ms linear`;
    existing.marker.setLatLng([a.lat, a.lon]);
    if (icon) {
      window.setTimeout(() => {
        if (icon) icon.style.transition = "none";
      }, GLIDE_MS + 100);
      const svg = icon.querySelector("svg");
      if (svg) svg.style.transform = `rotate(${a.track || 0}deg)`;
    }
    if (a.hex === selectedHex) updateDetailStats(a.hex);
    return;
  }

  const marker = L.marker([a.lat, a.lon], { icon: planeIcon(a.track) }).addTo(map);
  marker.on("click", () => openDetail(a.hex));
  aircraft.set(a.hex, { marker, missed: 0, data: a });
}

function pruneStale() {
  for (const [hex, entry] of aircraft) {
    if (entry.missed >= MISSED_POLL_LIMIT) {
      map.removeLayer(entry.marker);
      aircraft.delete(hex);
      if (hex === selectedHex) closeDetail();
    }
  }
}

async function poll() {
  for (const entry of aircraft.values()) entry.missed += 1;

  try {
    const url = `/api/aircraft?lat=${center.lat}&lon=${center.lon}&radius=${radiusNm}`;
    const resp = await fetch(url);
    if (!resp.ok) throw new Error(`status ${resp.status}`);
    const data = await resp.json();

    for (const a of data.aircraft || []) upsertAircraft(a);
    pruneStale();

    staleEl.classList.add("hidden");
    countEl.textContent = aircraft.size;
  } catch (err) {
    staleEl.classList.remove("hidden");
  }
}

function resetTracking() {
  for (const entry of aircraft.values()) map.removeLayer(entry.marker);
  aircraft.clear();
  closeDetail();
  countEl.textContent = "0";
}

function restartPolling() {
  if (pollTimer) clearInterval(pollTimer);
  poll();
  pollTimer = setInterval(poll, POLL_INTERVAL_MS);
}

radiusSelect.addEventListener("change", () => {
  radiusNm = Number(radiusSelect.value);
  resetTracking();
  restartPolling();
});

locateBtn.addEventListener("click", () => {
  if (!navigator.geolocation) {
    center = { ...ATHENS };
    map.setView([center.lat, center.lon], 8);
    resetTracking();
    restartPolling();
    return;
  }
  navigator.geolocation.getCurrentPosition(
    (pos) => {
      center = { lat: pos.coords.latitude, lon: pos.coords.longitude };
      map.setView([center.lat, center.lon], 9);
      resetTracking();
      restartPolling();
    },
    () => {
      center = { ...ATHENS };
      map.setView([center.lat, center.lon], 8);
      resetTracking();
      restartPolling();
    },
    { timeout: 8000 }
  );
});

restartPolling();
