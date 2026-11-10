// Names tabs after the session name of the coding agent running in them, exactly as
// the agent sets it (for Claude Code: the session title, including /rename).
// Draws nothing; it lives in a zero-size block of the layout and acts whenever
// Herdr's live data changes (titles, agents, tabs).
//
// A tab holding exactly one agent with a session name is renamed to it (24 chars
// max). A tab is only touched while it has Herdr's own number, the name this mod
// set, or its agent's session name (now or just before a rename), so a tab you
// named after the session follows /rename and any other name you type wins. When
// the agent leaves or a second one joins, the tab gets Herdr's number back.
//
// It also publishes every agent pane's session name (untruncated) as
// shared("auto-rename.titles") = { <pane id>: <name> }, so a pane header can show
// it without repeating these rules (the auto-rename-pane mod does).

import { View, div } from "gpui-kit";
import { spaces, agents, run, shared, setShared } from "herdr";

const MAX_TAB = 24;
const TITLES = "auto-rename.titles";

// The title rules match label.go in the herdr TUI plugin (repo root).
// Titles that are an agent's or shell's default rather than a session name.
const GENERIC = new Set(["claude", "claude code", "codex", "opencode", "gemini", "gemini cli", "pi", "zsh", "bash", "fish", "sh"]);
// Shell-prompt titles (user@host:~/dir) and bare paths.
const PROMPT_OR_PATH = /^[\w.-]+@[\w.-]+[: ]|^[~/]\S*$/;

// The agent's session name, or "" when its title doesn't look like one.
// Strips leading status glyphs agents put in titles (spinners, ✳).
export function sessionTitle(a) {
  const t = (a.title ?? "").replace(/^[^\p{L}\p{N}]+/u, "").trim();
  if (!t) return "";
  const low = t.toLowerCase();
  if (GENERIC.has(low) || low === (a.app ?? "").toLowerCase() || low === (a.program?.name ?? "").toLowerCase()) return "";
  if (PROMPT_OR_PATH.test(t)) return "";
  return t;
}

const truncate = (s, n) => ([...s].length > n ? [...s].slice(0, n - 1).join("") + "…" : s);

export default class AutoRename extends View {
  init() {
    // tab id -> the name this mod last set (kept across restarts).
    this.set = JSON.parse(localStorage.getItem("set") ?? "{}");
    // pane -> its session name at the last look, to follow a /rename.
    this.prev = {};
    // tab id -> name asked for and not yet echoed back, so a render doesn't ask twice.
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
          adopt = Boolean(want) && tab.named && (tab.name === want || (old && tab.name === old));
        }
        const mine = this.set[tab.id];
        const ours = tab.named && Boolean(mine) && tab.name === mine;
        if (tab.named && !ours && !adopt) {
          // Named by someone else: hands off, and forget any claim on it.
          if (mine) { delete this.set[tab.id]; dirty = true; }
          continue;
        }

        const target = want || ""; // "" = Herdr's number again
        const current = tab.named ? tab.name : "";
        if (current === target) {
          delete this.asked[tab.id];
          if (target && mine !== target) { this.set[tab.id] = target; dirty = true; }
          if (!target && mine) { delete this.set[tab.id]; dirty = true; }
          continue;
        }
        if (this.asked[tab.id] === target) continue;
        this.asked[tab.id] = target;
        if (target) this.set[tab.id] = target;
        else delete this.set[tab.id];
        dirty = true;
        run("tab.rename", { id: tab.id, name: target });
      }
    }

    for (const id of Object.keys(this.set)) if (!live.has(id)) { delete this.set[id]; dirty = true; }
    for (const id of Object.keys(this.asked)) if (!live.has(id)) delete this.asked[id];
    this.prev = titles;
    if (dirty) this.save();
  }

  render() {
    try { this.sync(); } catch (e) { console.log(`auto-rename: ${e?.message ?? e}`); }
    return div().size_full();
  }
}
