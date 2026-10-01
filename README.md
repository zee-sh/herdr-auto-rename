# herdr-pane-title

A [herdr](https://github.com/ogulcancelik/herdr) plugin that names tabs and pane borders after the
coding agent's **session name** (e.g. the Claude Code session title) instead of `1 2 3` / `claude`.

## How it works

Agents like Claude Code write their session name into the terminal title, which herdr exposes as
`terminal_title_stripped`. The plugin copies that title into:

- the **tab label**, for tabs holding exactly one agent (truncated to 24 characters);
- the pane's `display_agent` metadata (`herdr pane report-metadata`), which herdr draws on split
  pane borders.

It runs as a small background watcher, started by a `[[startup]]` hook (or the `refresh` action),
that subscribes to herdr's socket events. `pane.updated` fires the moment a terminal title changes,
so a Claude Code `/rename` shows up immediately. herdr doesn't offer that event to plugin hooks,
hence the watcher. It exits with the herdr server; one runs per herdr session.

- Tabs are only renamed while their label is herdr's default number or the name the plugin set,
  so a tab you rename yourself (`prefix+shift+t`) is never touched. When the agent exits or the tab
  gains a second agent, the tab gets its position number back.
- Pane labels never override a manual pane name (`prefix+shift+p`), and are scoped to the agent
  so herdr clears them when it exits.
- Generic titles (`claude`, `Claude Code`, the agent's own name) are not shown; if the title
  falls back to one of them, the plugin clears the label it set. Pane labels are truncated to
  40 characters.
- After `plugin install`/`link`/`enable`, run the `refresh` action once
  (`herdr plugin action invoke refresh --plugin zee-sh.pane-title`): startup hooks only run when
  the server starts.
- herdr only draws pane labels on split pane borders; a lone pane relies on the tab label.

## Requirements

- herdr ≥ 0.9.0, Go to build.
- In `config.toml`:

  ```toml
  [ui]
  show_agent_labels_on_pane_borders = true
  ```

## Install

```sh
herdr plugin install zee-sh/herdr-pane-title   # runs `go build` via [[build]]
# or, from a checkout:
make link      # build, (re)link and restart the watcher
make restart   # after a rebuild
```

Optional keybinding for the `refresh` action:

```toml
[[keys.command]]
key = "prefix+t"
type = "plugin_action"
command = "zee-sh.pane-title.refresh"
```

## Debugging

```sh
herdr plugin log list --plugin zee-sh.pane-title
touch "$(herdr plugin config-dir zee-sh.pane-title)/debug"   # log raw events
tail -f "$(herdr plugin config-dir zee-sh.pane-title)/events.log"
```
