# herdr-auto-rename

A [herdr](https://herdr.dev) plugin that names tabs and pane borders after each coding agent's
**session name**, exactly as the agent sets it (for Claude Code: the session title, including
`/rename`), and does nothing else. The same thing for the Herdr desktop app lives in
[`app/`](app/) (see [Install](#install)).

```
before:   1        2   3
after:    fix-auth 2   api-docs
```

## Features

- **The session name, as written**, including Claude Code's `/rename`.
- **Live**: a watcher listens to herdr's socket events, so renames show up immediately.
- **Agent tabs only**: tabs without an agent keep herdr's numbers.
- **Respects your names**: a tab is only renamed while it shows herdr's default number, a name
  this plugin set, or its agent's session name. Name a tab after its session and it follows
  `/rename`; give it any other name and it is left alone.
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
events, and exits with the server, or on its next event after the plugin is disabled or
unlinked. A `pane.agent_status_changed` hook restarts it if it ever dies.

## Requirements

- herdr ≥ 0.9.0, Go ≥ 1.22 to build, macOS or Linux. (The app side needs only the Herdr app.)
- For pane border labels:

  ```toml
  [ui]
  show_agent_labels_on_pane_borders = true
  ```

## Install

### herdr (TUI)

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

### Herdr app

Ask the agent in your Herdr window to *add the mod at
github.com/zee-sh/herdr-auto-rename/tree/main/app*. It follows [`app/RECIPE.md`](app/RECIPE.md):
copies two mods into `~/.config/herdr-app/mods/`, adds them to the layout you're using (cloning
it first if it's one of Herdr's own), and checks them. No keys, nothing to build, no programs
run, no network.

- **`auto-rename`** draws nothing. It sits in a 0px block of the layout and renames single-agent
  tabs on every render, with the same rules as the plugin.
- **`auto-rename-pane`** is Herdr's `pane` header with one change: an agent's pane shows its
  session name (40 characters max) instead of `Claude Code`.

The app knows which tabs someone named and can give a tab its number back, so the app side
needs no watcher and no position tracking. It runs only while a layout that contains it is
shown.

## Development

```sh
make link      # build, (re)link and restart the watcher
make restart   # after a rebuild
go test ./...
```

The app mods are plain files: copy `app/mods/*` into `~/.config/herdr-app/mods/` (it reloads
them live) and run `herdr-app check` on each.

Debugging:

```sh
herdr plugin log list --plugin zee-sh.auto-rename
touch "$(herdr plugin config-dir zee-sh.auto-rename)/debug"   # log hook event payloads
tail -f "$(herdr plugin config-dir zee-sh.auto-rename)/events.log"
```

The watcher logs to `watch.log` in the plugin's state dir, under `session-<hash>/`.

## License

[MIT](LICENSE)
