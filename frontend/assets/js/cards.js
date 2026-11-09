// Render kartu & grup. Data API hanya masuk lewat textContent / properti DOM.

import { withBusy } from "./busy.js";
import {
  OTP_TTL_SECONDS, cardColors, formatCountdown, formatQuota, formatRupiah,
  isBlank, maskMsisdn, remainingSeconds, textOrDash,
} from "./format.js";
import { groupNumbers } from "./order.js";
import { allNumbers, dataSignature, getProviders } from "./store.js";

const KIND_VERIFIED = "verified";
const KIND_PENDING = "pending";
const CLEANUP_MS = 1500;
const DELETE_ARM_MS = 4000;
const SELECTOR_MENU = "[data-menu]";
const SELECTOR_ACTION = "[data-action]";

const nodes = new Map();
const sections = new Map();
let handlers = {};

const groupsEl = document.getElementById("groups");
const emptyEl = document.getElementById("empty-state");
const fabEl = document.getElementById("btn-add-fab");

function field(root, name) {
  return root.querySelector(`[data-field="${name}"]`);
}

function cloneTemplate(id) {
  return document.getElementById(id).content.firstElementChild.cloneNode(true);
}

function syncChildren(parent, desired) {
  desired.forEach((node, index) => {
    if (parent.children[index] !== node) {
      parent.insertBefore(node, parent.children[index] ?? null);
    }
  });
  while (parent.children.length > desired.length) {
    parent.lastElementChild.remove();
  }
}

function setLogo(card, logo) {
  const img = field(card, "logo");
  if (isBlank(logo)) {
    img.hidden = true;
    return;
  }
  if (img.getAttribute("src") !== logo) {
    img.setAttribute("src", logo);
  }
  img.hidden = false;
}

function applyIdentity(card, item) {
  const { accent, ink } = cardColors(item.color);
  card.style.setProperty("--accent", accent);
  card.style.setProperty("--card-ink", ink);
  card.dataset.id = item.id;
  field(card, "label").textContent = textOrDash(item.label);
  field(card, "msisdn").textContent = maskMsisdn(item.msisdn);
  setLogo(card, item.logo);
}

function restartAnimationClass(card, className) {
  card.classList.remove(className);
  card.getBoundingClientRect(); // paksa reflow agar animasi mulai ulang
  card.classList.add(className);
  const clear = () => card.classList.remove(className);
  card.addEventListener("animationend", clear, { once: true });
  setTimeout(clear, CLEANUP_MS);
}

function markEntering(card) {
  card.classList.add("is-entering");
  const clear = () => card.classList.remove("is-entering");
  card.addEventListener("animationend", clear, { once: true });
  setTimeout(clear, CLEANUP_MS);
}

function warnTextOf(item) {
  if (item.sessionLost || item.error === "no_session") {
    return null;
  }
  if (item.warn) {
    return item.warn;
  }
  return item.ok === false && item.error ? String(item.error) : null;
}

function isEnded(item) {
  return item.sessionLost === true || item.error === "no_session";
}

function syncOverlay(card, selector, templateId, active) {
  const existing = card.querySelector(selector);
  if (!active) {
    existing?.remove();
    return null;
  }
  if (existing) {
    return existing;
  }
  const created = cloneTemplate(templateId);
  card.append(created);
  return created;
}

function syncStatus(card, item) {
  const ended = isEnded(item);
  syncOverlay(card, "[data-ribbon]", "tpl-card-ended", ended);
  const warnText = ended ? null : warnTextOf(item);
  const warn = syncOverlay(card, "[data-warn]", "tpl-card-warn", warnText !== null);
  if (warn) {
    field(warn, "warn-text").textContent = warnText;
  }
  if (ended) {
    card.dataset.state = "ended";
  } else {
    card.dataset.state = warnText === null ? "ok" : "warn";
  }
}

function fillData(card, item) {
  const data = item.data ?? {};
  field(card, "balance").textContent = formatRupiah(data.balance);
  field(card, "quota").textContent = formatQuota(data.quota, data.unit, data.unlimited);
  const used = field(card, "used");
  const hasUsed = !isBlank(data.used);
  used.hidden = !hasUsed;
  used.textContent = hasUsed ? `terpakai ${formatQuota(data.used, data.unit, false)}` : "";
  field(card, "active").textContent = textOrDash(data.active_until);
  field(card, "grace").textContent = textOrDash(data.grace_until);
  field(card, "bonstri").textContent = textOrDash(data.bonstri);
}

function createEntry(kind, templateId) {
  const el = cloneTemplate(templateId);
  markEntering(el);
  return { el, kind, sig: "" };
}

function updateVerified(entry, item) {
  const card = entry.el;
  applyIdentity(card, item);
  fillData(card, item);
  syncStatus(card, item);
  const sig = dataSignature(item.data);
  if (entry.sig !== "" && sig !== entry.sig) {
    restartAnimationClass(card, "is-shimmer");
  }
  entry.sig = sig;
}

function updatePending(entry, item) {
  applyIdentity(entry.el, item);
  entry.addedAt = item.added_at;
  paintPending(entry);
}

function paintPending(entry) {
  const card = entry.el;
  const left = remainingSeconds(entry.addedAt, Date.now());
  const expired = left <= 0;
  const ring = field(card, "ring");
  ring.style.setProperty("--progress", String(left / OTP_TTL_SECONDS));
  ring.toggleAttribute("data-expired", expired);
  field(card, "countdown").textContent = formatCountdown(left);
  const note = field(card, "pending-note");
  note.textContent = expired ? "Kedaluwarsa" : "Menunggu kode";
  note.toggleAttribute("data-expired", expired);
  card.querySelector('[data-action="resend"]').hidden = !expired;
  card.querySelector('[data-action="continue"]').hidden = expired;
}

function entryFor(item) {
  const kind = item.pending ? KIND_PENDING : KIND_VERIFIED;
  const existing = nodes.get(item.id);
  if (existing?.kind === kind) {
    return existing;
  }
  existing?.el.remove();
  const created = kind === KIND_PENDING ? createEntry(kind, "tpl-card-pending") : createEntry(kind, "tpl-card");
  nodes.set(item.id, created);
  return created;
}

function renderItem(item) {
  const entry = entryFor(item);
  if (entry.kind === KIND_PENDING) {
    updatePending(entry, item);
  } else {
    updateVerified(entry, item);
  }
  return entry.el;
}

function sectionFor(group) {
  let section = sections.get(group.key);
  if (!section) {
    section = cloneTemplate("tpl-group");
    sections.set(group.key, section);
  }
  field(section, "group-label").textContent = textOrDash(group.label);
  field(section, "group-count").textContent = String(group.verifiedCount);
  const { accent } = cardColors(group.color);
  section.style.setProperty("--group-accent", accent);
  syncChildren(field(section, "group-cards"), group.items.map(renderItem));
  return section;
}

function pruneStale(items, groups) {
  const liveIds = new Set(items.map((item) => item.id));
  for (const [id, entry] of [...nodes.entries()]) {
    if (!liveIds.has(id)) {
      entry.el.remove();
      nodes.delete(id);
    }
  }
  const liveKeys = new Set(groups.map((group) => group.key));
  for (const key of [...sections.keys()]) {
    if (!liveKeys.has(key)) {
      sections.delete(key);
    }
  }
}

export function render() {
  const items = allNumbers();
  const groups = groupNumbers(items, getProviders());
  const desired = groups.map(sectionFor);
  syncChildren(groupsEl, desired);
  pruneStale(items, groups);
  const hasAny = items.length > 0;
  emptyEl.hidden = hasAny;
  fabEl.hidden = !hasAny;
}

export function tick() {
  for (const entry of nodes.values()) {
    if (entry.kind === KIND_PENDING) {
      paintPending(entry);
    }
  }
}

const armTimers = new WeakMap();

function disarm(button) {
  clearTimeout(armTimers.get(button));
  armTimers.delete(button);
  if (button.dataset.armed === "1") {
    delete button.dataset.armed;
    button.textContent = button.dataset.label ?? button.textContent;
  }
}

function removeMenu(menu) {
  menu.querySelectorAll("button").forEach(disarm);
  menu.remove();
}

function closeMenus(except) {
  document.querySelectorAll(SELECTOR_MENU).forEach((menu) => {
    if (menu.parentElement !== except) {
      menu.parentElement.querySelector('[data-action="menu"]')?.setAttribute("aria-expanded", "false");
      removeMenu(menu);
    }
  });
}

function toggleMenu(card, button) {
  const open = card.querySelector(SELECTOR_MENU);
  if (open) {
    removeMenu(open);
    button.setAttribute("aria-expanded", "false");
    return;
  }
  closeMenus(card);
  card.append(cloneTemplate("tpl-card-menu"));
  button.setAttribute("aria-expanded", "true");
  card.querySelector(`${SELECTOR_MENU} button`)?.focus();
}

function closeMenuOf(card) {
  const menu = card.querySelector(SELECTOR_MENU);
  if (menu) {
    removeMenu(menu);
    const button = card.querySelector('[data-action="menu"]');
    button?.setAttribute("aria-expanded", "false");
  }
}

function armDelete(button) {
  if (button.dataset.armed === "1") {
    return true;
  }
  button.dataset.label = button.textContent;
  button.dataset.armed = "1";
  button.textContent = "Yakin hapus?";
  armTimers.set(button, setTimeout(() => disarm(button), DELETE_ARM_MS));
  return false;
}

async function runAction(action, button, card) {
  if (action === "menu") {
    toggleMenu(card, button);
    return;
  }
  if (action === "delete" && !armDelete(button)) {
    return;
  }
  const handler = handlers[action];
  if (!handler) {
    return;
  }
  await withBusy(button, () => handler(card.dataset.id));
  closeMenuOf(card);
}

function onRootClick(event) {
  const button = event.target.closest(SELECTOR_ACTION);
  const card = button?.closest("[data-card]");
  if (!button || !card) {
    return;
  }
  runAction(button.dataset.action, button, card).catch(() => {
    button.disabled = false;
    button.removeAttribute("aria-busy");
  });
}

export function bindCardActions(actionHandlers) {
  handlers = actionHandlers;
  groupsEl.addEventListener("click", onRootClick);
  document.addEventListener("click", (event) => {
    if (!event.target.closest("[data-card]")) {
      closeMenus(null);
    }
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      const trigger = document.querySelector('[data-action="menu"][aria-expanded="true"]');
      closeMenus(null);
      trigger?.focus();
    }
  });
}
