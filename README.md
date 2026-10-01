# herdr-auto-rename

A [herdr](https://herdr.dev) plugin that names tabs and pane borders after each coding agent's
**session name**, exactly as the agent sets it (for Claude Code: the session title, including
`/rename`), and does nothing else.

```
before:   1   2   3
after:    Luvus documentation   2   herdr-plugin-rename
```

## Features

- **The session name, as written**, including Claude Code's `/rename`.
- **Live**: a watcher listens to herdr's socket events, so renames show up immediately.
- **Agent tabs only**: tabs without an agent keep herdr's numbers.
- **Respects your names**: a tab is only renamed while it shows herdr's default number or a name
  this plugin set. Rename a tab yourself and it is left alone.
- **Pane labels, not pane renames**: pane labels are herdr display metadata scoped to the agent.
  They never override a pane name you set and disappear when the agent exits.
- **One watcher per herdr session**, with separate state for each named session.

## How it works

Agents write their session name into the terminal title, which herdr exposes as
`terminal_title_stripped`. Titles that are not session names (`claude`, `Claude Code`, a shell
prompt like `user@host:~/dir`, a bare path) are ignored.

- **Tabs** holding exactly one agent are renamed to its session name (24 characters max). When
  the agent leaves or a second agent joins, the tab shows its position number again. herdr can't
  return a renamed tab to automatic numbering, so the plugin keeps that number in step as tabs
  are opened, closed and moved.
- **Panes** get the session name as `display_agent` metadata (40 characters max), which herdr
  shows on split pane borders and in the agent sidebar.

A `[[startup]]` hook starts one watcher per herdr session. It subscribes to `pane.updated` (which
herdr does not offer to plugin hooks; it fires on title changes) plus pane and tab lifecycle
events, and exits with the server. A `pane.agent_status_changed` hook restarts it if it ever dies.

## Requirements

- herdr ≥ 0.9.0, Go ≥ 1.22 to build, macOS or Linux.
- For pane border labels:

  ```toml
  [ui]
  show_agent_labels_on_pane_borders = true
  ```

## Install

```sh
herdr plugin install zee-sh/herdr-auto-rename        # builds with go
herdr plugin action invoke restart --plugin zee-sh.auto-rename
```

herdr only runs startup hooks when its server starts, so the second line starts the plugin now.
Run it again after upgrading. Optional keybinding:

```toml
[[keys.command]]
key = "prefix+t"
type = "plugin_action"
command = "zee-sh.auto-rename.refresh"
```

## Development

```sh
make link      # build, (re)link and restart the watcher
make restart   # after a rebuild
go test ./...
```

Debugging:

```sh
herdr plugin log list --plugin zee-sh.auto-rename
touch "$(herdr plugin config-dir zee-sh.auto-rename)/debug"   # log hook event payloads
tail -f "$(herdr plugin config-dir zee-sh.auto-rename)/events.log"
```

The watcher logs to `watch.log` in the plugin's state dir, under `session-<hash>/`.
