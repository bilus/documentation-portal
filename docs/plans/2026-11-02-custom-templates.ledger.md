# Custom templates and assets: ledger

## Design (2026-10-30, agreed in conversation)

- `portal.WithCustomTemplates(fs.FS)`: each top-level `*.html` replaces the built-in template of the same name; missing ones fall back; extra files are loaded for overrides to call.
- Startup check: `New` refuses templates that do not parse or fail to render sample pages.
- `portal.WithCustomAssets(fs.FS)`: files served at `{base}/assets/custom/`.
- docportal: `-templates` / `DOCPORTAL_TEMPLATES`, `-assets` / `DOCPORTAL_ASSETS`.
- Chat page (templ) stays out of scope.
- Implementer's calls: an `fs.FS` with no `*.html` is refused; each handler carries its own template set; `Unpublished` takes the options and returns `(http.Handler, error)` (breaking, so the next tag is v0.5.0).

## Review (2026-11-02)

One sub-agent ran the AGENTS.md prompt: 5 tool calls, 1 experiment, ~89k tokens, 2m17s.

| # | Finding | Severity | Confirmed | Decision |
| :--- | :--- | :--- | :--- | :--- |
| 1 | Custom assets follow symlinks out of an `os.DirFS` directory, serve dotfiles and list directories | high | reviewer's experiment, and mine | Fixed: assets handler serves regular files only, 404 for directories, dot segments and unopenable paths; docportal opens both directories with `os.OpenRoot` |
| 2 | Sample check runs every page against an empty page no request brings, refusing `{{(index .Sidebar 0).Title}}` in `doc.html` | medium | reviewer's experiment | Fixed: per-page samples a request can bring, times each banner and with/without account links |
| 3 | `nav.html` without `{{define "nav"}}` is accepted and has no effect | medium | reviewer's experiment | Fixed: refused, naming the `define` to add |
| 4 | `Unpublished` routes through `ServeMux`, which redirects `//x` instead of answering 503 | low | reviewer's experiment | Fixed: dispatch by prefix without `ServeMux` |
| 5 | `-templates`/`-assets` error drops the cause | low | code | Fixed: wraps the `os.Stat` error |

## Wrong implementations

| Name | Before fixes | After fixes |
| :--- | :--- | :--- |
| M1 only the empty / last sample | survived | caught |
| M2 only the full / first sample | survived | caught |
| M3 `viewer.html` unchecked | survived | caught |
| M4 ended-banner sample dropped | survived | caught |
| M5 only the first sample's errors count | survived | caught |
| M6 dir validation `break` instead of `continue` | caught | caught |
| M7 `-assets` checked for existence only | caught | caught |
| M8 sample check before parsing the custom templates | caught | n/a (code moved) |
| M9 no partial check (mine) | — | caught |
| M10 dotfiles allowed (mine) | — | caught |
| M11 directories served (mine) | — | survived, then caught after a real subdirectory was added to the docportal test |
| M12 `os.DirFS` for assets (mine) | — | caught |
