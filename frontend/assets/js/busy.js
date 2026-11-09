// Menandai tombol sibuk selama request agar tidak bisa ditekan dua kali.

export function setBusy(button, busy) {
  button.disabled = busy;
  if (busy) {
    button.setAttribute("aria-busy", "true");
  } else {
    button.removeAttribute("aria-busy");
  }
}

export async function withBusy(buttons, task) {
  const list = (Array.isArray(buttons) ? buttons : [buttons]).filter(Boolean);
  if (list.some((button) => button.disabled)) {
    return undefined;
  }
  list.forEach((button) => setBusy(button, true));
  try {
    return await task();
  } finally {
    list.forEach((button) => setBusy(button, false));
  }
}
