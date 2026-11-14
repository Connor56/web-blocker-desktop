const tokenKey = "annoyingWebBlockerToken";
const fragment = new URLSearchParams(location.hash.slice(1));
if (fragment.has("token")) {
  localStorage.setItem(tokenKey, fragment.get("token"));
  history.replaceState(null, "", location.pathname);
}

const token = localStorage.getItem(tokenKey) || "";
const form = document.querySelector("#settings");
const sites = document.querySelector("#sites");
const status = document.querySelector("#status");
const message = document.querySelector("#message");
const save = document.querySelector("#save");
const dialog = document.querySelector("#challenge-dialog");
const dot = document.querySelector("#dot");
const challengeTime = document.querySelector("#challenge-time");
const clickTime = document.querySelector("#click-time");
const challengeNote = document.querySelector("#challenge-note");
let pendingConfig = null;
let challengeId = "";
let challengeTimer = null;
let restartingChallenge = false;

function setStatus(active) {
  status.textContent = active ? "At least one website is blocked now" : "No website is blocked right now";
  status.classList.toggle("active", active);
}

async function api(path, method, body, unlockToken = "") {
  const response = await fetch(path, {
    method,
    headers: {
      "Authorization": `Bearer ${token}`,
      ...(body ? {"Content-Type": "application/json"} : {}),
      ...(unlockToken ? {"X-Change-Unlock": unlockToken} : {})
    },
    body: body ? JSON.stringify(body) : undefined
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(payload.error || `Request failed (${response.status})`);
    error.status = response.status;
    error.payload = payload;
    throw error;
  }
  return payload;
}

function field(labelText, type, value) {
  const label = document.createElement("label");
  label.textContent = labelText;
  const input = document.createElement("input");
  input.type = type;
  input.value = value;
  input.required = true;
  label.append(input);
  return {label, input};
}

function addSchedule(container, schedule = {start: "09:00", end: "17:00"}) {
  const row = document.createElement("div");
  row.className = "schedule";
  const start = field("Start", "time", schedule.start);
  const end = field("End", "time", schedule.end);
  const remove = document.createElement("button");
  remove.type = "button";
  remove.className = "danger small";
  remove.textContent = "Remove time";
  remove.addEventListener("click", () => row.remove());
  row.append(start.label, end.label, remove);
  container.append(row);
}

function addSiteCard(site = {domain: "", enabled: true, schedules: []}) {
  const card = document.createElement("section");
  card.className = "site";
  const head = document.createElement("div");
  head.className = "site-head";
  const domain = field("Exact web address", "text", site.domain);
  domain.input.placeholder = "youtube.com";
  domain.input.className = "domain";
  const enabledLabel = document.createElement("label");
  enabledLabel.className = "toggle";
  const enabled = document.createElement("input");
  enabled.type = "checkbox";
  enabled.checked = site.enabled;
  enabled.className = "enabled";
  enabledLabel.append(enabled, " Enabled");
  const remove = document.createElement("button");
  remove.type = "button";
  remove.className = "danger small";
  remove.textContent = "Delete website";
  remove.addEventListener("click", () => card.remove());
  head.append(domain.label, enabledLabel, remove);
  const scheduleList = document.createElement("div");
  scheduleList.className = "schedules";
  site.schedules.forEach(schedule => addSchedule(scheduleList, schedule));
  const add = document.createElement("button");
  add.type = "button";
  add.className = "secondary small";
  add.textContent = "Add schedule";
  add.addEventListener("click", () => addSchedule(scheduleList));
  card.append(head, scheduleList, add);
  sites.append(card);
}

function render(config) {
  sites.replaceChildren();
  config.sites.forEach(addSiteCard);
}

function readConfig() {
  return {
    sites: [...sites.querySelectorAll(".site")].map(card => ({
      domain: card.querySelector(".domain").value.trim(),
      enabled: card.querySelector(".enabled").checked,
      schedules: [...card.querySelectorAll(".schedule")].map(row => {
        const inputs = row.querySelectorAll("input");
        return {start: inputs[0].value, end: inputs[1].value};
      })
    }))
  };
}

async function load() {
  if (!token) {
    status.textContent = "Open this page by running the Annoying Web Blocker executable.";
    form.hidden = true;
    return;
  }
  try {
    const payload = await api("/api/config", "GET");
    render(payload.config);
    setStatus(payload.status ? payload.status.active : payload.active);
    if (payload.error) message.textContent = payload.error;
  } catch (error) {
    status.textContent = "Could not connect securely to the blocker.";
    message.textContent = error.message;
  }
}

document.querySelector("#add-site").addEventListener("click", () => addSiteCard());

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  save.disabled = true;
  message.textContent = "";
  pendingConfig = readConfig();
  try {
    const payload = await api("/api/config", "PUT", pendingConfig);
    render(payload.config);
    setStatus(payload.active);
    message.style.color = "#267326";
    message.textContent = "Schedule saved.";
    refreshStatus();
  } catch (error) {
    if (error.status === 423 && error.payload.challengeRequired) {
      await startChallenge();
    } else {
      message.style.color = "#b42318";
      message.textContent = error.message;
    }
  } finally {
    save.disabled = false;
  }
});

function moveDot() {
  dot.style.left = `${Math.random() * 78 + 2}%`;
  dot.style.top = `${Math.random() * 70 + 2}%`;
}

async function startChallenge(restarted = false) {
  try {
    const state = await api("/api/unlock/start", "POST", {});
    challengeId = state.id;
    updateChallengeTimers(state.remainingMs, state.clickWithinMs);
    moveDot();
    challengeNote.textContent = restarted ? "You missed the five-second timer. The full challenge has restarted." : "";
    if (!dialog.open) dialog.showModal();
  } catch (error) {
    clearInterval(challengeTimer);
    if (dialog.open) dialog.close();
    message.textContent = error.message;
  }
}

function updateChallengeTimers(totalMs, clickMs) {
  clearInterval(challengeTimer);
  const totalEnds = performance.now() + totalMs;
  const clickEnds = performance.now() + clickMs;
  const renderTimers = () => {
    const totalLeft = Math.max(0, totalEnds - performance.now());
    const clickLeft = Math.max(0, clickEnds - performance.now());
    challengeTime.textContent = `Unlock timer: ${Math.ceil(totalLeft / 1000)} seconds`;
    clickTime.textContent = `Click again within: ${(clickLeft / 1000).toFixed(1)} seconds`;
    clickTime.style.color = clickLeft <= 1500 ? "#b42318" : "";
    if (clickLeft <= 0 && !restartingChallenge) {
      restartingChallenge = true;
      clearInterval(challengeTimer);
      startChallenge(true).finally(() => { restartingChallenge = false; });
    }
  };
  renderTimers();
  challengeTimer = setInterval(renderTimers, 100);
}

dot.addEventListener("click", async () => {
  dot.disabled = true;
  try {
    const state = await api("/api/unlock/click", "POST", {id: challengeId});
    if (state.unlockToken) {
      clearInterval(challengeTimer);
      dialog.close();
      const payload = await api("/api/config", "PUT", pendingConfig, state.unlockToken);
      render(payload.config);
      setStatus(payload.active);
      message.style.color = "#267326";
      message.textContent = "Weaker schedule saved after challenge.";
      pendingConfig = null;
    } else {
      updateChallengeTimers(state.remainingMs, state.clickWithinMs);
      moveDot();
    }
  } catch (error) {
    if (error.status === 409) {
      restartingChallenge = true;
      clearInterval(challengeTimer);
      await startChallenge(true);
      restartingChallenge = false;
    } else {
      clearInterval(challengeTimer);
      dialog.close();
      message.style.color = "#b42318";
      message.textContent = error.message;
    }
  } finally {
    dot.disabled = false;
  }
});

document.querySelector("#cancel-challenge").addEventListener("click", () => {
  clearInterval(challengeTimer);
  dialog.close();
  message.textContent = "Change canceled; the stronger schedule remains saved.";
});

load();

async function refreshStatus() {
  if (!token) return;
  try {
    const state = await api("/api/status", "GET");
    if (state.error) { status.textContent = `Enforcement error: ${state.error}`; status.classList.remove("active"); }
    else if (state.checked) setStatus(state.active);
  } catch (error) { status.textContent = "Cannot reach blocker; enforcement status is unknown."; }
}
refreshStatus();
setInterval(refreshStatus, 5000);
