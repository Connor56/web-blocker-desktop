const tokenKey = "annoyingWebBlockerToken";
const fragment = new URLSearchParams(location.hash.slice(1));
if (fragment.has("token")) {
  localStorage.setItem(tokenKey, fragment.get("token"));
  history.replaceState(null, "", location.pathname);
}

const token = localStorage.getItem(tokenKey) || "";
const form = document.querySelector("#settings");
const enabled = document.querySelector("#enabled");
const start = document.querySelector("#start");
const end = document.querySelector("#end");
const domains = document.querySelector("#domains");
const status = document.querySelector("#status");
const message = document.querySelector("#message");
const save = document.querySelector("#save");

function setStatus(active) {
  status.textContent = active ? "Blocking is active now" : "Blocking is not active right now";
  status.classList.toggle("active", active);
}

async function api(method, body) {
  const response = await fetch("/api/config", {
    method,
    headers: {
      "Authorization": `Bearer ${token}`,
      ...(body ? {"Content-Type": "application/json"} : {})
    },
    body: body ? JSON.stringify(body) : undefined
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error || `Request failed (${response.status})`);
  return payload;
}

async function load() {
  if (!token) {
    status.textContent = "Open this page by running the Annoying Web Blocker executable.";
    form.hidden = true;
    return;
  }
  try {
    const payload = await api("GET");
    enabled.checked = payload.config.enabled;
    start.value = payload.config.start;
    end.value = payload.config.end;
    domains.value = payload.config.domains.join("\n");
    setStatus(payload.status ? payload.status.active : payload.active);
    if (payload.error) message.textContent = payload.error;
  } catch (error) {
    status.textContent = "Could not connect securely to the blocker.";
    message.textContent = error.message;
  }
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  save.disabled = true;
  message.textContent = "";
  try {
    const payload = await api("PUT", {
      enabled: enabled.checked,
      start: start.value,
      end: end.value,
      domains: domains.value.split(/\r?\n|,/).map(value => value.trim()).filter(Boolean)
    });
    domains.value = payload.config.domains.join("\n");
    setStatus(payload.status ? payload.status.active : payload.active);
    message.style.color = "#267326";
    message.textContent = "Schedule saved.";
    refreshStatus();
  } catch (error) {
    message.style.color = "#b42318";
    message.textContent = error.message;
  } finally {
    save.disabled = false;
  }
});

load();

async function refreshStatus() {
  if (!token) return;
  try {
    const response = await fetch('/api/status', {headers: {Authorization: `Bearer ${token}`}});
    if (!response.ok) throw new Error('Status unavailable');
    const state = await response.json();
    if (state.error) { status.textContent = `Enforcement error: ${state.error}`; status.classList.remove('active'); }
    else if (state.checked) setStatus(state.active);
  } catch (error) { status.textContent = 'Cannot reach blocker; enforcement status is unknown.'; }
}
refreshStatus();
setInterval(refreshStatus, 5000);
