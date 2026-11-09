// Bottom sheet tambah nomor + OTP, dan dialog konfirmasi sesi berakhir.

import { ApiError, api, messageOf, numberPath } from "./api.js";
import { withBusy } from "./busy.js";
import {
  OTP_TTL_SECONDS, formatCountdown, isHexColor, maskMsisdn, normalizeMsisdn, remainingSeconds, textOrDash,
} from "./format.js";
import { findByMsisdn, getNumber, getProviders, removeNumber, subscribe, touchPending, upsertPending } from "./store.js";

const STATUS_WAITING = "Masukkan kode sebelum waktu habis.";
const STATUS_EXPIRED = "Kode kedaluwarsa. Kirim ulang.";
const STATUS_RESENT = "Kode baru dikirim.";
const HTTP_CONFLICT = 409;
const HTTP_BAD_REQUEST = 400;
const HTTP_NOT_FOUND = 404;

const byId = (id) => document.getElementById(id);
const el = {
  sheet: byId("sheet"),
  title: byId("sheet-title"),
  formAdd: byId("form-add"),
  formOtp: byId("form-otp"),
  msisdn: byId("input-msisdn"),
  operatorList: byId("operator-list"),
  operatorOptions: byId("operator-options"),
  addError: byId("add-error"),
  addSubmit: byId("btn-add-submit"),
  ring: byId("otp-ring"),
  countdown: byId("otp-countdown"),
  target: byId("otp-target"),
  status: byId("otp-status"),
  otp: byId("input-otp"),
  otpError: byId("otp-error"),
  verify: byId("btn-otp-verify"),
  resend: byId("btn-otp-resend"),
  cancel: byId("btn-otp-cancel"),
  confirm: byId("dialog-confirm"),
  confirmNumber: byId("confirm-number"),
  confirmOk: byId("btn-confirm-ok"),
};

let deps = { reload: async () => undefined, showError: () => undefined };
let current = null;
let timer = null;
let reauthTarget = null;

function checkedProvider() {
  return el.formAdd.querySelector('input[name="provider"]:checked')?.value;
}

function showMessage(node, text) {
  node.textContent = text ?? "";
  node.hidden = !text;
}

function stopTimer() {
  if (timer !== null) {
    clearInterval(timer);
    timer = null;
  }
}

function paintOtp() {
  if (current === null) {
    return;
  }
  const left = remainingSeconds(current.added_at, Date.now());
  const expired = left <= 0;
  el.ring.style.setProperty("--progress", String(left / OTP_TTL_SECONDS));
  el.ring.toggleAttribute("data-expired", expired);
  el.countdown.textContent = formatCountdown(left);
  if (expired) {
    el.status.textContent = STATUS_EXPIRED;
  }
}

function openDialog() {
  if (!el.sheet.open) {
    el.sheet.showModal();
  }
}

function renderOperators(selected) {
  const template = byId("tpl-operator");
  const options = getProviders()
    .filter((provider) => provider.available === true)
    .map((provider) => {
      const option = template.content.firstElementChild.cloneNode(true);
      const radio = option.querySelector('[data-field="radio"]');
      radio.value = provider.id;
      radio.checked = provider.id === selected;
      option.querySelector('[data-field="label"]').textContent = textOrDash(provider.label);
      const logo = option.querySelector('[data-field="logo"]');
      logo.hidden = !provider.logo;
      if (provider.logo) {
        logo.setAttribute("src", provider.logo);
      }
      if (isHexColor(provider.color)) {
        option.style.setProperty("--option-accent", provider.color.trim());
      }
      return option;
    });
  el.operatorOptions.replaceChildren(...options);
  if (options.length === 0) {
    showMessage(el.addError, "Belum ada operator yang tersedia.");
  }
}

function showAddForm(prefill) {
  stopTimer();
  current = null;
  el.formOtp.hidden = true;
  el.formAdd.hidden = false;
  el.msisdn.readOnly = false;
  el.addSubmit.hidden = false;
  el.title.textContent = "Tambah nomor";
  el.msisdn.value = prefill?.msisdn ?? "";
  el.otp.value = "";
  showMessage(el.addError, "");
  showMessage(el.otpError, "");
  renderOperators(prefill?.provider);
}

export function openAdd(prefill) {
  showAddForm(prefill);
  openDialog();
  el.msisdn.focus();
}

export function openOtp(item, statusText) {
  stopTimer();
  current = item;
  // Nomor + operator tetap terlihat; memilih operator lain membatalkan pending (5.2).
  el.formAdd.hidden = false;
  el.msisdn.value = item.msisdn;
  el.msisdn.readOnly = true;
  el.addSubmit.hidden = true;
  showMessage(el.addError, "");
  renderOperators(item.provider);
  el.formOtp.hidden = false;
  el.title.textContent = "Masukkan kode";
  el.target.textContent = maskMsisdn(item.msisdn);
  el.status.textContent = statusText ?? STATUS_WAITING;
  el.otp.value = "";
  showMessage(el.otpError, "");
  paintOtp();
  timer = setInterval(paintOtp, 1000);
  openDialog();
  el.otp.focus();
}

function pendingFrom(res, fallbackMsisdn) {
  return {
    id: res.id,
    provider: res.provider,
    label: res.label,
    color: res.color,
    logo: res.logo ?? null,
    msisdn: res.msisdn ?? fallbackMsisdn,
    added_at: res.added_at ?? Date.now() / 1000,
  };
}

async function finish() {
  el.sheet.close();
  await deps.reload();
}

async function finishExisting(msisdn) {
  await finish();
  const existing = findByMsisdn(normalizeMsisdn(msisdn));
  if (existing && (existing.sessionLost || existing.error === "no_session")) {
    confirmReauth(existing);
  }
}

async function handleAddResult(res, msisdn) {
  const needsCode = res.status === "otp_sent" || res.status === "otp_resent" || res.otp_required === true;
  if (!needsCode) {
    await finishExisting(msisdn);
    return;
  }
  const item = pendingFrom(res, msisdn);
  upsertPending(item);
  openOtp(item);
}

async function submitAdd(event) {
  event.preventDefault();
  if (current !== null) {
    return;
  }
  const msisdn = el.msisdn.value.trim();
  const provider = checkedProvider() ?? "";
  if (msisdn === "" || provider === "") {
    showMessage(el.addError, msisdn === "" ? "Masukkan nomor ponsel." : "Pilih operator.");
    return;
  }
  showMessage(el.addError, "");
  await withBusy([el.addSubmit, el.operatorList], async () => {
    try {
      await handleAddResult(await api.post("/numbers", { provider, msisdn }), msisdn);
    } catch (error) {
      if (error instanceof ApiError && error.status === HTTP_CONFLICT) {
        await finishExisting(msisdn);
        return;
      }
      showMessage(el.addError, messageOf(error));
    }
  });
}

async function submitOtp(event) {
  event.preventDefault();
  const code = el.otp.value.trim();
  if (current === null || code === "") {
    showMessage(el.otpError, "Masukkan kode verifikasi.");
    return;
  }
  showMessage(el.otpError, "");
  await withBusy([el.verify, el.resend, el.cancel], async () => {
    try {
      await api.post(numberPath(current.id, "/verify"), { otp: code });
      await finish();
    } catch (error) {
      showMessage(el.otpError, messageOf(error));
    }
  });
}

function backToAdd(item) {
  removeNumber(item.id);
  openAdd({ msisdn: item.msisdn, provider: item.provider });
}

// Kirim ulang untuk nomor pending (dari form OTP maupun dari kartu).
export async function resendById(item) {
  try {
    const res = await api.postEmpty(numberPath(item.id, "/resend"));
    const addedAt = res.added_at ?? Date.now() / 1000;
    touchPending(item.id, addedAt);
    await deps.reload();
    openOtp(getNumber(item.id) ?? { ...item, added_at: addedAt }, STATUS_RESENT);
  } catch (error) {
    if (error instanceof ApiError && error.status === HTTP_BAD_REQUEST) {
      backToAdd(item);
      return;
    }
    throw error;
  }
}

async function resendFromForm() {
  const item = current;
  if (item === null) {
    return;
  }
  await withBusy([el.verify, el.resend, el.cancel], async () => {
    try {
      await resendById(item);
    } catch (error) {
      showMessage(el.otpError, messageOf(error));
    }
  });
}

// Hapus nomor pending / nomor apa pun. 404 dianggap sudah hilang.
export async function deleteNumber(id) {
  try {
    await api.del(numberPath(id));
  } catch (error) {
    if (!(error instanceof ApiError) || error.status !== HTTP_NOT_FOUND) {
      throw error;
    }
  }
  removeNumber(id);
}

async function cancelFromForm() {
  const item = current;
  if (item === null) {
    return;
  }
  await withBusy([el.verify, el.resend, el.cancel], async () => {
    try {
      await deleteNumber(item.id);
      el.sheet.close();
    } catch (error) {
      showMessage(el.otpError, messageOf(error));
    }
  });
}

async function onOperatorChange() {
  showMessage(el.addError, "");
  if (current === null) {
    return;
  }
  const waiting = current;
  const chosen = checkedProvider();
  await withBusy([el.operatorList, el.verify, el.resend, el.cancel], async () => {
    try {
      await deleteNumber(waiting.id);
    } catch (error) {
      renderOperators(waiting.provider);
      showMessage(el.addError, messageOf(error));
      return;
    }
    showAddForm({ provider: chosen });
  });
}

export function confirmReauth(item) {
  reauthTarget = item;
  el.confirmNumber.textContent = maskMsisdn(item.msisdn);
  if (!el.confirm.open) {
    el.confirm.showModal();
  }
}

async function acceptReauth() {
  const item = reauthTarget;
  if (item === null) {
    return;
  }
  await withBusy(el.confirmOk, async () => {
    try {
      await deleteNumber(item.id);
    } catch (error) {
      el.confirm.close();
      deps.showError(messageOf(error));
      return;
    }
    el.confirm.close();
    openAdd({ msisdn: item.msisdn, provider: item.provider });
  });
}

function guard(task) {
  return (event) => {
    task(event).catch((error) => deps.showError(messageOf(error)));
  };
}

function refillOperators() {
  const empty = el.operatorOptions.children.length === 0;
  if (empty && el.sheet.open && current === null && getProviders().length > 0) {
    renderOperators(undefined);
  }
}

export function initSheet(options) {
  deps = options;
  subscribe(refillOperators);
  el.formAdd.addEventListener("submit", guard(submitAdd));
  el.formOtp.addEventListener("submit", guard(submitOtp));
  el.resend.addEventListener("click", guard(resendFromForm));
  el.cancel.addEventListener("click", guard(cancelFromForm));
  el.operatorOptions.addEventListener("change", guard(onOperatorChange));
  byId("btn-sheet-close").addEventListener("click", () => el.sheet.close());
  el.sheet.addEventListener("close", stopTimer);
  byId("btn-add-empty").addEventListener("click", () => openAdd());
  byId("btn-add-fab").addEventListener("click", () => openAdd());
  el.confirmOk.addEventListener("click", guard(acceptReauth));
  byId("btn-confirm-cancel").addEventListener("click", () => el.confirm.close());
}
