// Pengelompokan & pengurutan kartu (FRONTEND.md 5.4). Fungsi murni.

function byAddedAt(a, b) {
  return Number(a.added_at) - Number(b.added_at);
}

function byLabel(a, b) {
  return String(a.label).localeCompare(String(b.label));
}

function countVerified(items) {
  return items.filter((item) => !item.pending).length;
}

function makeGroup(key, source, items) {
  return {
    key,
    label: source.label,
    color: source.color,
    logo: source.logo,
    items: [...items].sort(byAddedAt),
    verifiedCount: countVerified(items),
  };
}

export function groupNumbers(numbers, providers) {
  const known = providers.filter((p) => p.available === true);
  const knownIds = new Set(known.map((p) => p.id));
  const groups = [];

  for (const provider of known) {
    const items = numbers.filter((n) => n.provider === provider.id);
    if (items.length > 0) {
      groups.push(makeGroup(provider.id, provider, items));
    }
  }

  const orphans = new Map();
  for (const item of numbers) {
    if (!knownIds.has(item.provider)) {
      const list = orphans.get(item.provider) ?? [];
      list.push(item);
      orphans.set(item.provider, list);
    }
  }
  const tail = [...orphans.entries()].map(([id, items]) => makeGroup(`orphan:${id}`, items[0], items));
  tail.sort(byLabel);
  return [...groups, ...tail];
}
