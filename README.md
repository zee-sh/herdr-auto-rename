# herdr-pane-title

A [herdr](https://github.com/ogulcancelik/herdr) plugin that labels each agent pane's border with the
agent's **session name** (e.g. the Claude Code session title) instead of just `claude`.

## How it works

Agents like Claude Code write their session name into the terminal title, which herdr exposes as
`terminal_title_stripped`. On `pane.agent_detected` and `pane.agent_status_changed` the plugin
copies that title into the pane's `display_agent` metadata via `herdr pane report-metadata`.

- Non-destructive: a manual pane name (`prefix+shift+p`) still wins, and the label is scoped to the
  agent so it clears when the agent exits.
- Generic titles (`claude`, `Claude Code`, the agent's own name) are ignored; long titles are
  truncated to 40 characters.
- Renames show up on the next state change (every agent turn).

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
make link
```

Optional manual refresh keybinding:

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
