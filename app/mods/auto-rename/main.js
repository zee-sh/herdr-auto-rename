// Names tabs after the session name of the coding agent running in them, exactly as
// the agent sets it (for Claude Code: the session title, including /rename).
// Draws nothing; it lives in a zero-size block of the layout and acts whenever
// Herdr's live data changes (titles, agents, tabs).
//
// A tab holding exactly one agent with a session name is renamed to it (24 chars
// max). A tab is only touched while it has Herdr's own number, the name this mod
// set, its agent's default name (the + menu names a new tab "Claude Code"), or its
// agent's session name (now or just before a rename), so a tab you named after the
// session follows /rename and any other name you type wins. When the agent leaves
// or a second one joins, the tab gets Herdr's number back.
//
// It also publishes every agent pane's session name (untruncated) as
// shared("auto-rename.titles") = { <pane id>: <name> }, so a pane header can show
// it without repeating these rules (the auto-rename-pane mod does).

import { View, div } from "gpui-kit";
import { spaces, agents, machines, run, shared, setShared } from "herdr";

const MAX_TAB = 24;
const TITLES = "auto-rename.titles";
// A rename not echoed back by then is asked for again (its machine may have been away).
const RETRY_MS = 5000;

// The title rules match label.go in the herdr TUI plugin (repo root). The TUI hands
// label.go a title with the agent's status glyph already stripped; the app doesn't,
// so a leading run of symbols followed by a space (✳, spinners) is dropped first.
// Titles that are an agent's or shell's default rather than a session name.
const GENERIC = new Set(["claude", "claude code", "codex", "opencode", "gemini", "gemini cli", "pi", "zsh", "bash", "fish", "sh"]);
// Shell-prompt titles (user@host:~/dir) and bare paths.
const PROMPT_OR_PATH = /^[\w.-]+@[\w.-]+[: ]|^[~/]\S*$/;
const STATUS_GLYPH = /^[^\p{L}\p{N}\s]+\s+/u;

// The agent's session name, or "" when its title doesn't look like one.
export function sessionTitle(a) {
  const raw = (a.title ?? "").trim();
  const t = raw.replace(STATUS_GLYPH, "").trim();
  if (!t) return "";
  const low = t.toLowerCase();
  if (GENERIC.has(low) || low === (a.app ?? "").toLowerCase() || low === (a.program?.name ?? "").toLowerCase()) return "";
  if (PROMPT_OR_PATH.test(raw) || PROMPT_OR_PATH.test(t)) return "";
  return t;
}

const truncate = (s, n) => ([...s].length > n ? [...s].slice(0, n - 1).join("") + "…" : s);

export default class AutoRename extends View {
  init() {
    // tab id -> the name this mod set and saw land (kept across restarts).
    this.set = JSON.parse(localStorage.getItem("set") ?? "{}");
    // pane -> its session name at the last look, to follow a /rename.
    this.prev = {};
    // tab id -> { names: every name asked for since the last one landed, target, at }.
    // Those names are ours too, so a title that changes again before the
    // rename lands doesn't look like a name someone typed.
    this.asked = {};
  }

  save() {
    localStorage.setItem("set", JSON.stringify(this.set));
  }

  sync() {
    const byTab = new Map();
    const titles = {};
    for (const a of agents()) {
      if (!byTab.has(a.tab)) byTab.set(a.tab, []);
      byTab.get(a.tab).push(a);
      const t = sessionTitle(a);
      if (t) titles[a.pane] = t;
    }
    // setShared re-renders every mod, this one too: only publish a change.
    if (JSON.stringify(titles) !== JSON.stringify(shared(TITLES) ?? {})) setShared(TITLES, titles);

    const now = Date.now();
    const live = new Set();
    let dirty = false;

    for (const s of spaces()) {
      for (const tab of s.tabs ?? []) {
        live.add(tab.id);
        const inTab = byTab.get(tab.id) ?? [];
        let want = "";
        let adopt = false;
        if (inTab.length === 1) {
          const a = inTab[0];
          want = truncate(sessionTitle(a), MAX_TAB);
          const old = truncate(this.prev[a.pane] ?? "", MAX_TAB);
          // The session name (now or before a /rename), or the agent's default name.
          adopt = Boolean(want) && tab.named &&
            (tab.name === want || (old && tab.name === old) || sessionTitle({ ...a, title: tab.name }) === "");
        }
        const mine = this.set[tab.id];
        const pending = this.asked[tab.id];
        const ours = tab.named && ((Boolean(mine) && tab.name === mine) || Boolean(pending?.names.has(tab.name)));
        if (tab.named && !ours && !adopt) {
          // Named by someone else: hands off, and forget any claim on it.
          if (mine) { delete this.set[tab.id]; dirty = true; }
          delete this.asked[tab.id];
          continue;
        }

        const target = want; // "" = Herdr's number again
        const current = tab.named ? tab.name : "";
        if (current === target) {
          delete this.asked[tab.id];
          if (target && mine !== target) { this.set[tab.id] = target; dirty = true; }
          if (!target && mine) { delete this.set[tab.id]; dirty = true; }
          continue;
        }
        if (pending?.target === target && now - pending.at < RETRY_MS) continue;
        this.asked[tab.id] = { names: (pending?.names ?? new Set()).add(target), target, at: now };
        run("tab.rename", { id: tab.id, name: target });
      }
    }

    // A tab missing from spaces() may only belong to a machine still loading or
    // away: forget tabs only when every machine's layout is in.
    if (machines().every((m) => m.layout === "ready")) {
      for (const id of Object.keys(this.set)) if (!live.has(id)) { delete this.set[id]; dirty = true; }
    }
    for (const id of Object.keys(this.asked)) if (!live.has(id)) delete this.asked[id];
    this.prev = titles;
    if (dirty) this.save();
  }

  // Taken out of the layout: stop the pane header showing names nobody updates.
  async close() {
    setShared(TITLES, {});
  }

  render() {
    try { this.sync(); } catch (e) { console.log(`auto-rename: ${e?.message ?? e}`); }
    return div().size_full();
  }
}
