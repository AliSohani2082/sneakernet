# TUI / CLI UX research — Sneakernet

*Research report, 2026-10-09. Read-only on source; line numbers refer to the working tree at the time of writing.*

Scope: `sneakernet tui` (`internal/tui`) and the `sneakernet` CLI (`cmd/sneakernet`). This report collects TUI/CLI UX techniques, Charm-ecosystem libraries and lessons from well-known TUIs, then turns them into prioritized changes for this repo.

Hard constraints for every recommendation:

- **Offline.** Nothing is downloaded at runtime. New Go dependencies are fine because they are compiled into the static binary at build time.
- **Bare live-ISO consoles.** `TERM=linux`, 8/16 colors, a 256/512-glyph console font (no braille, no emoji, no nerd fonts), and possibly no mouse. It must also work over serial consoles and pipes.
- **Runs over sudo.** `env_reset` drops most of the environment, including `NO_COLOR` and `COLORTERM`. `TERM` survives.

---

## 1. Current state

### 1.1 CLI (`cmd/sneakernet`)

| Aspect | Today |
|---|---|
| Parsing | stdlib `flag`, one `FlagSet` per command, dispatch map in `run()` (`main.go:54`). Global usage is a hand-written string (`main.go:27`). |
| Commands | `install tui status list add remove switch test doctor uninstall convert version` |
| Output | A small `ui` type (`ui.go`): `step/ok/warn/fail` prefixes (`==>`, ` ok`, ` !!`, `ERR`), raw SGR colors, numbered menus (`choose`) and `[default]` prompts (`ask`). Everything goes to **one writer (stdout)**, errors included. |
| Color | Off unless stdout is a char device, `NO_COLOR` is empty and `TERM != dumb` (`ui.go:29`). There is no `--no-color`. `NO_COLOR` is lost when `requireRoot` re-runs the command under sudo (`main.go:134`). |
| Root | `requireRoot` re-execs `sudo <self> <args>`. **Read-only commands (`status`, `list`) need root too**, so `sneakernet status` asks for a password even though the README shows it without `sudo`. |
| Exit codes | 0 ok; 2 for no args or an unknown command; **1 for everything else**, including flag/usage errors, "not installed", a failed `test` and `doctor` problems. |
| Non-interactive | `install --yes` plus `--server/--routing/--region/--target/--*-port`. `uninstall --yes`. When stdin is not a TTY the prompts still read it and fail with `errInputClosed`, and they don't say which flag to pass. PRD FR-31 `--ui` and `--dry-run` are not implemented (US-8 quotes `--ui tui`). |
| Help | Global usage plus default `flag` output for `-h`. No examples per command. `help <cmd>` prints the global usage. No "did you mean". |
| Machine output | None, apart from `convert` (Xray JSON). No `--json` for `status/list/test/doctor`. |
| Completions / man | None. |
| Stale hint | `install.go:466` says "press **T** to test them all", but the key is `^t`. |

### 1.2 TUI (`internal/tui`)

Bubble Tea v2.0.10, bubbles v2.2.1, lipgloss v2.0.6.

| Aspect | Today |
|---|---|
| Screens | Search (main), Add (textarea with a live plan preview), Logs (viewport, manual refresh). Opens on Add when the list is empty. Pasting links into search opens Add. |
| Input model | Letters always go to the search box, so every action is a **ctrl chord** (`^t ^b ^a ^n ^x ^o ^l ^r ^s`). `esc` clears the search, and pressed again it quits. |
| Discoverability | One `help.ShortHelpView` line with 11 bindings (`keys.go:48`). At 80 columns it is cut off with `…`, so the later bindings (logs, restart, start/stop, esc) are hidden. There is no full-help toggle and no command list. |
| Feedback | One status line: spinner (`spinner.MiniDot`, braille) + message, or an inline `y/N` confirmation. Messages stay until the next one replaces them. **Busy work can't be cancelled**: a 10 s test blocks every action. |
| Testing | `^t` runs `probe.All`, which returns only after **all** servers finish. No progress or partial results until then. |
| Layout | Hand-built strings. Fixed `kindW = 30` column. Name width `w-59` (21 cells at 80 columns). `listHeight = height-9`. No narrow or short layouts. |
| Styling | Package-level styles using ANSI 16 colors (`view.go:20`). Good for consoles. The help bubble keeps `help.DefaultDarkStyles()`, which uses hex greys (`#626262`, `#4A4A4A`, `#3C3C3C`). |
| Glyphs | `› ● ○ … · ─` and braille spinner frames. Server names often have emoji flags (`pad()` mentions 2-cell flags). |
| Mouse | Off. |
| Alt screen | On (`View.AltScreen = true`). |
| Tests | `tui_test.go` drives the model directly (no golden render tests). |

### 1.3 What is already good (keep it)

- The plain, line-based installer works over serial consoles, pipes and screen readers, and the tests can script it. This is the right default for a live-ISO tool.
- The search-first main screen with name-before-property ranking, `field:value` qualifiers and typo tolerance. It is already fzf-class.
- ANSI 16-color palette instead of hex/truecolor. It inherits the console palette and degrades well.
- Color is never the only signal: `●` active marker, `failed:` text, `ms` numbers, `·reason` columns.
- Destructive actions confirm first (`^x`, `uninstall`).
- The installer summary gives copy-pasteable `export` lines and next commands.

---

## 2. Prioritized recommendations

Effort: **S** < ½ day, **M** 1–3 days, **L** > 3 days. Impact is for the target users (stressed people on a filtered network, often on a bare console).

| # | Item | Location | Library / technique | Effort | Impact |
|---|---|---|---|---|---|
| **Quick wins** |||||
| 1 | Fix the stale "press T" hint | `cmd/sneakernet/install.go:466` | text | S | Med |
| 2 | **Console-safe "tty mode"**: ASCII glyphs + `spinner.Line` + flag→`[DE]` on `TERM=linux` / non-UTF-8 locale / `--ascii` | `internal/tui/view.go`, `tui.go:144` | btop-style tty mode; `bubbles/spinner.Line` | S | **High** |
| 3 | Own help-bar styles in ANSI colors (default greys are likely to downsample to dark grey/black on the VT) | `tui.go:142` (`help.New()`) | `help.Styles` with `lipgloss.Color("7")`, `Bold` | S | High |
| 4 | **Full help overlay** (`F1` / `ctrl+g`), grouped, plus a short bar that ends with "F1 help" | `keys.go`, `view.go` | `help.KeyMap` (`ShortHelp`/`FullHelp`), `help.ShowAll` | S | **High** |
| 5 | Context-aware bindings: disable bindings that can't run now so the help hides them | `keys.go`, `tui.go` (`refresh`, `onKey`) | `key.Binding.SetEnabled` (help skips disabled) | S | Med |
| 6 | F-key aliases (mc/htop convention) for consoles and for tmux/screen users whose prefix is `^b`/`^a` | `keys.go:17` | `key.WithKeys("ctrl+t","f5")` | S | Med |
| 7 | `esc` cancels running work (test/switch) | `tui.go` (`startBusy`, `testResults`, `doSwitch`) | per-op `context.WithCancel` | S | High |
| 8 | Status messages auto-clear after a few seconds (errors stay until a key is pressed) | `tui.go:233` `setMsg` | `tea.Tick` with a message id | S | Low |
| 9 | Confirm before discarding pasted links on the Add screen | `tui.go:642` | existing `confirmation` | S | Med |
| 10 | `tui` with no TTY: friendly error that points to `list`/`switch` | `commands.go:417` | `term.IsTerminal` (`charmbracelet/x/term`, already indirect) | S | Med |
| 11 | `add` with a TTY on stdin: print "paste links, then Ctrl-D" instead of waiting silently | `commands.go:141` | isatty check | S | Med |
| 12 | Errors and progress to **stderr**, results to stdout | `ui.go` (`ui.err`), `main.go:54` | clig.dev convention | S | Med |
| 13 | Distinct **exit codes** (usage 2, not installed 3, no root 4, connectivity 5, interrupted 130) | `main.go:54`, `commands.go` | typed errors | S | Med |
| 14 | `--no-color` / `--color=auto\|always\|never`, kept across the sudo re-exec; same `NO_COLOR` rule in CLI and TUI | `ui.go:29`, `main.go:134`, `tui.go:37` | `tea.WithColorProfile(colorprofile.Ascii)` | S | Med |
| 15 | Never prompt when stdin isn't a TTY: fail with the flag to pass; add `--no-input` | `ui.go:70` `ask` | clig.dev "never require a prompt" | S | Med |
| 16 | Installer: print the **equivalent non-interactive command** at the end | `install.go:470` `printSummary` | — | S | High |
| 17 | `help <cmd>`, per-command examples, "did you mean …?" | `main.go` | Levenshtein (or cobra built-in) | S | Med |
| **Medium** |||||
| 18 | **Live test results**: rows fill in as each server answers, plus a progress count/bar; CLI `test --all` streams | `internal/probe/probe.go:82`, `tui.go:463`, `commands.go:224` | callback/channel + `bubbles/progress`; `tea.View.ProgressBar` | M | **High** |
| 19 | **Responsive columns** (≥100 / 80–99 / <80) and a compact layout for short terminals | `view.go:69` `searchView`, `row` | width breakpoints | M | High |
| 20 | **`--json`** for `status`, `list`, `test`, `doctor`, `version` | `commands.go` | `encoding/json` | M | Med |
| 21 | Human error messages with a hint (a cause → fix table) used by both CLI and TUI | new `internal/explain` (or `cmd/sneakernet/errors.go`) + `tui.go` `setMsg` callers | clig.dev "rewrite errors for humans" | M | High |
| 22 | Command palette: `:` at the start of the search box lists actions with their shortcuts | `tui.go` `searchKey`, `view.go` | k9s `:` mode; reuse `sahilm/fuzzy` | M | Med |
| 23 | Logs screen: follow mode, level colors, `/` search | `tui.go:691`, `view.go:226` | `viewport.SetHighlights`, `HighlightNext` | M | Med |
| 24 | `status`/`list` without sudo via a non-secret `status.json` (0644) | `internal/manage`, `commands.go:47` | — | M | Med |
| 25 | Installer polish: step counter `[3/6]`, recap + confirm before writing | `install.go:28` | — | S–M | Med |
| 26 | Aligned CLI tables that measure display width (emoji/CJK) | `ui.go:163` `serverTable`, `commands.go:264` `printResults` | `lipgloss/v2/table` (borderless) | S–M | Low–Med |
| 27 | Mouse: wheel scroll and click-to-select, opt-in | `view.go:30`, `tui.go` | `View.MouseMode = tea.MouseModeCellMotion`, `tea.MouseWheelMsg` | S–M | Low |
| 28 | Golden render tests at 60/80/120 columns × unicode/tty mode | `internal/tui/tui_test.go` | `charmbracelet/x/exp/golden` (already in mod cache) | M | Med (guards 2, 19) |
| **Larger** |||||
| 29 | Move the CLI to **cobra + fang**: styled help, completions, man page, suggestions, `--version` | `cmd/sneakernet/*` | `github.com/spf13/cobra`, `charm.land/fang/v2` | L | Med–High |
| 30 | Install shell completions offline (bash/zsh/fish), recorded in the manifest | `internal/install`, `internal/layout` | cobra `GenBashCompletionV2` etc. at install time | M (after 29) | Med |
| 31 | FR-31 gaps: `--ui`, `--dry-run` on `install` | `install.go:28` | — | M | Med |
| **Evaluated, not recommended now** |||||
| — | huh forms for the installer | — | `charm.land/huh/v2` | — | Revisit at M2/M3, when the installer needs multi-field forms |
| — | glamour, harmonica, gum, charm `log` | — | — | — | see §4 |

Suggested order: 1–17 (one or two PRs, all S). Then 18 + 7 (testing UX), 19 + 2 + 28 (console robustness), 20/21. Then 29/30 if completions are wanted.

---

## 3. Details

### R1. Fix the stale hint — S

`install.go:466`: `(press T to test them all)` → `(press ^t to test them all)`. This message appears when the first connection fails, so the user is already stuck when they read it. Add a test that greps user-facing strings for keys that are not in `newKeyMap()`, or build hint text from `keys.Test.Help().Key`.

### R2. Console-safe "tty mode" — S, high impact

**Problem.** The Linux VT keeps at most 512 glyphs. Unicode characters without a glyph are shown as a replacement (often `?` or a diamond) [kernel LKML thread, Wikipedia]. The braille frames of `spinner.MiniDot` (`⠋⠙⠹…`) are not in any stock console font. The emoji flags in server names (`🇩🇪`) are not either. `ansi.StringWidth` counts a flag as 2 cells, and the VT draws something else, so columns can misalign. `›`, `●`, `○`, `…` depend on the console font loaded by the distro. Box drawing `─` and `░▌█` are in CP437 and are safe.

**Prior art.** btop has `-t/--tty_on`, which it also turns on automatically on a TTY: "max 16 colors and tty-friendly graph symbols" [btop man page].

**Change.** Add a glyph set chosen once at startup. Turn on tty mode when `TERM=linux`, when the locale is not UTF-8 (`LC_ALL`/`LC_CTYPE`/`LANG` without `UTF-8`), or when `SNEAKERNET_ASCII=1` / `--ascii` is given (`TERM` survives sudo; `LANG` usually does as well).

```go
// internal/tui/glyphs.go
type glyphs struct {
	cursor, active, inactive, sep, ellipsis string
	spinner                                 spinner.Spinner
}

var (
	unicodeGlyphs = glyphs{"› ", "●", "○", " · ", "…", spinner.MiniDot}
	ttyGlyphs     = glyphs{"> ", "*", "o", " | ", "~", spinner.Line} // Line = | / - \
)

func detectGlyphs(env func(string) string) glyphs {
	if env("SNEAKERNET_ASCII") != "" || env("TERM") == "linux" || !utf8Locale(env) {
		return ttyGlyphs
	}
	return unicodeGlyphs
}
```

Also in tty mode:
- Replace regional-indicator pairs with `[DE]` in **display only** (a `displayName(s)` helper used by `row()` and `details()`; keep `s.Name` unchanged for search). Two runes in U+1F1E6..U+1F1FF → `[` + `'A'+(r-0x1F1E6)` … `]`. Remove other emoji (variation selectors, pictographs) or replace them with `?`.
- Pass the ellipsis glyph to every `ansi.Truncate(..., "…")` call (`view.go` uses `"…"` 6 times). `help.Model.Ellipsis` is a field too.
- `─` in `rule()` can stay.

Check on real live ISOs (Debian/Ubuntu/Fedora/Arch) with `showconsolefont`. Font sets differ by distro (`Lat2-Terminus16`, `default8x16`, `eurlatgr`), so test rather than assume.

### R3. Help-bar contrast — S

`help.New()` uses `DefaultDarkStyles()`: key `#626262`, description `#4A4A4A`, separator `#3C3C3C` (bubbles `help/help.go:48`). On the ANSI-16 profile these hex greys are downsampled to the nearest palette entry. That is most likely bright-black (dark grey) for the text and black for the separator, which is hard to read or invisible on a black VT. This is an estimate; check it on a VT. The rest of the UI already uses ANSI indices, so do the same for help:

```go
h := help.New()
h.Styles.ShortKey = lipgloss.NewStyle().Bold(true)          // default fg, bright on VT
h.Styles.ShortDesc = lipgloss.NewStyle()                     // default fg
h.Styles.ShortSeparator = lipgloss.NewStyle().Faint(true)
h.Styles.FullKey, h.Styles.FullDesc = h.Styles.ShortKey, h.Styles.ShortDesc
h.Styles.Ellipsis = h.Styles.ShortSeparator
```

Another option is `tea.RequestBackgroundColor` → `tea.BackgroundColorMsg.IsDark()` → `help.DefaultStyles(isDark)`. The Linux VT doesn't answer OSC 11, though, so ANSI indices are the more robust choice.

### R4. Full help overlay + short bar — S, high impact

lazygit and k9s both show context-specific help on `?` [lazygit, k9s docs]. Here `?` is a valid search character, so use **`F1`** (works on every console) and **`ctrl+g`** (nano's help key). Make `keyMap` implement `help.KeyMap`:

```go
func (k keyMap) ShortHelp() []key.Binding { // 5–6 most used + the way out
	return []key.Binding{k.Use, k.Test, k.Best, k.Add, k.Help, k.Clear}
}
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Clear},   // Navigate
		{k.Use, k.Test, k.Best, k.Auto, k.Sort},          // Servers
		{k.Add, k.Remove},                                // List
		{k.Logs, k.Restart, k.Power, k.Quit},             // Service / app
	}
}
// view: m.help.View(m.keys) — ShowAll toggled by k.Help
```

In full-help mode, put the full help in place of the result list (or below it if there is room), and add a line explaining `field:value` qualifiers (`sec:reality proto:vless port:443 host:… sni:…`). Today that syntax appears only in the placeholder and disappears once the user types.

### R5. Context-aware bindings — S

`help.ShortHelpView` skips bindings where `!kb.Enabled()` (bubbles `help/help.go:137`). After each `refresh()`/status update:

```go
sel := m.selected()
m.keys.Use.SetEnabled(sel != nil && sel.Usable())
m.keys.Remove.SetEnabled(sel != nil)
m.keys.Test.SetEnabled(len(m.usableHits()) > 0)
m.keys.Power.SetHelp("^s", map[bool]string{true: "stop", false: "start"}[m.status.Active])
```

The bar then only offers what works, and "start/stop" names the action it will actually do.

### R6. F-key aliases — S

`^b` is the tmux prefix and `^a` is the GNU screen prefix, so users inside a multiplexer can't use "use fastest" or "auto". F-keys work on the Linux console and are the mc/htop convention people already know from consoles. Add them as extra keys and leave the ctrl chords in place:

| Action | ctrl | F-key |
|---|---|---|
| help | `^g` | `F1` |
| add | `^n` | `F2` |
| logs | `^l` | `F3` |
| test | `^t` | `F5` |
| use fastest | `^b` | `F6` |
| auto | `^a` | `F7` |
| remove | `^x` | `F8` |
| quit | `^c` | `F10` |

Bubble Tea puts the terminal in raw mode (IXON off), so `^s` doesn't freeze the console. No change is needed there.

### R7. Cancellable busy work — S

`startBusy` sets `m.busy`, and every action then answers "busy … wait a moment". A test of many dead servers takes up to `probeTimeout` (10 s). Keep a cancel func:

```go
func (m *model) startBusy(what string) (context.Context, tea.Cmd) {
	ctx, cancel := context.WithCancel(m.ctx)
	m.busy, m.cancel = what, cancel
	return ctx, m.spin.Tick
}
// searchKey: if m.busy != "" && key.Matches(k, m.keys.Clear) { m.cancel(); m.setMsg(false, "cancelled"); }
```

`probe.All` and `mgr.Switch` already take a context. When busy, show `esc cancel` in the status line.

### R8–R9. Message lifetime, Add-screen discard — S

- Give each `setMsg` a sequence id and return `tea.Tick(4*time.Second, …)` that clears it only if the id is unchanged. Errors stay until the next key.
- `addKey` Cancel when `m.plan != nil && len(m.plan.New) > 0`: reuse `confirmation{prompt: "Discard N pasted links? (y/N)"}`. Pasted REALITY links are long, and losing them by accident is costly.

### R10–R11. TTY guards — S

```go
// cmdTUI
if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
	return usageErr("the TUI needs a terminal; use: sneakernet list | switch <n> | test --all")
}
// cmdAdd, no file args
if f, ok := u.in.(*os.File); ok && term.IsTerminal(f.Fd()) { // keep the *os.File around in ui
	u.printf("Paste share links, then press Ctrl-D on an empty line.\n")
}
```

### R12. stdout vs stderr — S

clig.dev: "primary output … and anything machine-readable to stdout"; "log messages, errors … to stderr" [clig.dev]. Add `err io.Writer` to `ui`. Send `step/ok/warn/fail/dim` notes to it, and keep `serverTable`, `status` lines, `printResults` and `convert` JSON on `out`. `run(ctx, args, in, out, errOut)` changes signature, and `main_test.go` passes a second buffer (tests that assert on combined text can use the same buffer for both). Check color for each stream (`newUI` only checks `out`).

### R13. Exit codes — S

| Code | Meaning | Where it comes from |
|---|---|---|
| 0 | success | — |
| 1 | operation failed (generic) | default |
| 2 | usage: bad flag, wrong arg count, unknown command | `fs.Parse` errors, `"usage: …"` errors |
| 3 | not installed | `installed()` |
| 4 | needs root and no sudo / sudo refused | `requireRoot` |
| 5 | no connectivity (`test`, `status --check`, `doctor` network checks) | `cmdTest`, `cmdDoctor` |
| 130 | interrupted (SIGINT) | `ctx.Err() == context.Canceled` in `run` |

```go
type codedError struct{ code int; err error }
func usageErr(f string, a ...any) error { return &codedError{2, fmt.Errorf(f, a...)} }
// run(): var ce *codedError; if errors.As(err, &ce) { u.fail("%v", ce.err); return ce.code }
```

Document the codes in `sneakernet -h` and the README.

### R14. Color control across sudo — S

- Add a global `--color=auto|always|never` (`--no-color` = never) and `SNEAKERNET_COLOR`.
- In `requireRoot`, when color is off (because of `NO_COLOR` or the flag), add `--color=never` to the re-exec args. `sudo` resets the environment by default and drops `NO_COLOR`.
- Use one rule for both UIs. `newUI` treats any non-empty `NO_COLOR` as "off" (the no-color.org spec). `colorprofile` uses `strconv.ParseBool`, so `NO_COLOR=yes` has no effect on the TUI (`colorprofile/env.go:116`). Have the TUI decide the same way and pass it explicitly:

```go
opts := []tea.ProgramOption{tea.WithContext(ctx)}
if !colorOn { opts = append(opts, tea.WithColorProfile(colorprofile.Ascii)) } // keeps bold/faint, no colors
tea.NewProgram(m, opts...)
```

### R15. Non-interactive safety — S

`ask` reads from stdin even when it's a pipe. Following clig.dev ("only prompt if stdin is a TTY … never require a prompt"):

```go
func (u *ui) ask(question, def string) (string, error) {
	if u.yes { … }
	if !u.interactive { // set from isatty(stdin) or --no-input
		return "", usageErr("%s: no terminal to ask on; pass it as a flag (see -h) or use --yes", question)
	}
	…
}
```

Keep scripted-stdin tests working with a hidden `--input-from-stdin` or by setting `u.interactive` in `run` for tests. Today's tests type answers through a pipe, so this needs a test hook.

### R16. "Next time, run:" — S, high impact

A live session is gone after a reboot, so users re-run the installer often. At the end of `printSummary`:

```
  Next time, skip the questions:
    sudo sh /run/media/$USER/Ventoy/sneakernet/install.sh --yes --server auto --routing bypass-region --region ir
```

Build it from the final `manage.State` and the flags (using `selectionArg`, `st.Routing`, `st.Region`, and ports only when they are not the defaults). Also write it to `/var/log/sneakernet-install.log`.

### R17. Help text and suggestions — S

- `sneakernet help switch` → same as `sneakernet switch -h`.
- Each `FlagSet` gets a `Usage` with a one-line synopsis, **examples first**, then flags (clig.dev: "lead with examples"):

```
usage: sneakernet switch <number|auto>

  sudo sneakernet switch 12      use server #12 (see: sneakernet list)
  sudo sneakernet switch auto    keep testing all servers, use the fastest
```

- Unknown command → `unknown command "stauts". Did you mean "status"?` (Levenshtein ≤ 2 over the command names; cobra has this built in, see R29).

### R18. Live test progress — M, high impact

Today `probe.All` returns one map at the end (`probe.go:82`). Add a streaming hook. The rest of the function stays the same:

```go
// probe
type Options struct{ OnResult func(index int, r Result) } // called from worker goroutines
func All(ctx context.Context, xrayBin, assetDir string, servers []links.Server,
	probeURL string, timeout time.Duration, opts ...Options) (map[int]Result, error)
```

TUI: the standard Bubble Tea "listen on a channel" pattern:

```go
type resultMsg struct{ key string; r probe.Result }
type testDoneMsg struct{ err error; useBest bool }

func waitResult(ch <-chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

// testResults: ch := make(chan tea.Msg, len(cands)); start probe.All in a goroutine whose
// OnResult sends resultMsg{…} and finally testDoneMsg; return tea.Batch(startBusy, waitResult(ch))
// Update(resultMsg): m.results[k] = r; m.done++; re-sort if bySpeed; return m, waitResult(m.ch)
```

Status line: `testing 12/40 · 7 ok · fastest Berlin-2 84 ms   esc cancel`, optionally with `bubbles/progress` (`progress.New(progress.WithWidth(20), progress.WithoutPercentage())`). In tty mode use `progress.WithFillCharacters('#', '-')`, though the defaults `▌`/`░` are CP437-safe. Also set `v.ProgressBar` in `View()`. Bubble Tea v2 sends it as the terminal's native progress indicator (OSC 9;4) where supported, and terminals that don't support it ignore it.

CLI `test --all`: on a TTY, print each result line as it arrives, then the sorted summary. When piped, print only the sorted list. No animation when stdout isn't a TTY (clig.dev).

### R19. Responsive layout — M

At 80×24, the most common console size, the name column is 21 cells and the protocol column is 30. Use breakpoints in `searchView`/`row`:

| Width | Columns |
|---|---|
| ≥ 100 | cursor, active, #, name, kind (30), note (16) — as today |
| 80–99 | kind shortened to protocol + security (`vless/reality`, ~14) — name gains ~16 cells |
| < 80 | no kind column; kind shown in the `details()` line for the selected row |
| height < 14 | no header line 2 or details line; short help bar only |

Compute `nameW` from what is left after the columns that are shown. Cover these sizes with golden tests (R28).

### R20. `--json` — M

Keep stdout limited to the JSON document. Diagnostics go to stderr (R12). Suggested shapes:

```go
type statusJSON struct {
	Service  struct{ State string `json:"state"`; Active, Enabled bool; Since string } `json:"service"`
	Server   struct{ Auto bool `json:"auto"`; Index int `json:"index,omitempty"`; Name string `json:"name,omitempty"` } `json:"server"`
	Routing  string `json:"routing"`; Region string `json:"region,omitempty"`
	Socks    string `json:"socks"`; HTTP string `json:"http"`        // "127.0.0.1:10808"
	Check    *struct{ OK bool; LatencyMS int64 `json:"latency_ms"`; Error string `json:"error,omitempty"` } `json:"check,omitempty"`
}
// list:   [{"index":12,"name":"…","kind":"vless ws/tls","usable":true,"problem":"","active":false}]
// test:   [{"index":12,"ok":true,"latency_ms":84}|{"index":3,"ok":false,"error":"timed out"}]
// doctor: {"ok":false,"checks":[{"name":"service running","ok":false,"error":"…"}]}
```

Never put share links (credentials) in `list --json` unless `--with-links` is given.

### R21. Errors with hints — M

Many messages are raw errors (`switch failed: exit status 1`, `probe xray did not start: …`). Add one table, shared by CLI and TUI:

```go
type hint struct{ match func(error) bool; msg, fix string }
var hints = []hint{
	{isAddrInUse, "port is already in use", "another proxy running? pick ports: --socks-port/--http-port"},
	{isTimeout, "timed out", "this server is blocked or down — ^t tests all, ^b uses the fastest"},
	{isSystemctlFail, "the service did not start", "see logs: ^l  (or journalctl -u sneakernet-xray)"},
	{isXrayConfigTest, "Xray rejected the config", "run: sudo sneakernet doctor"},
}
func explain(err error) (msg, fix string)
```

The TUI shows `msg`, then a dim `fix`. The CLI prints `ERR msg` followed by `    fix`. clig.dev: "catch expected errors and rewrite them for humans … put the most important information last".

### R22. Command palette — M

Ctrl chords are hard to discover, and a palette is how users who don't read help find features. Following k9s's `:` mode: when the search box **starts with `:`**, the list shows commands instead of servers, ranked by `sahilm/fuzzy`. Each row shows its shortcut (`test results  ^t / F5`), so the palette also teaches the keys. `enter` runs the command and clears the box. Server names hardly ever start with `:`, so there is little conflict. Build the entries from the `keyMap` (one `{binding, run}` per action) so the help, palette and shortcuts can't drift apart. Add a placeholder hint: `… or : for commands`.

### R23. Logs screen — M

- Follow mode: while `screen == logsScreen`, refetch on the existing 3 s `tickMsg`, and stay at the bottom only if the user was already at the bottom (`m.logs.AtBottom()`).
- Color Xray levels: `[Warning]` → warn style, `[Error]`/`failed` → err style (line-prefix regexp, ANSI colors).
- `/` opens a small textinput. Matches go to `m.logs.SetHighlights(ranges)`; `n`/`N` → `HighlightNext/Previous` (bubbles v2 viewport API).
- Header shows `following` / `paused`.

### R24. Read-only commands without sudo — M

`status` asks for a password only because `state.json` is 0600. Write a **non-secret** `status.json` 0644 (ports, routing, selection index/name, no links) in `manage.Apply`/`Switch`. Then `status` works without root, and `systemctl is-active` is unprivileged. Fall back to sudo only for `--check`, or when the file is missing. Trade-off: server *names* become readable by local users. Make it opt-out, or show only the index.

### R25. Installer polish — S–M

- Number the steps: `==> [2/6] Checking the files on the stick`. This helps users who are reading slowly on a console.
- After the questions and before writing anything, show a recap: `Install to this system · TUI · server auto · routing bypass-region (ir) · ports 10808/10809. Continue? (Y/n)`. Skip it with `--yes`.
- Keep it line-based. Don't use the alt screen or cursor tricks (see §4, huh).

### R26. CLI tables — S–M

`serverTable` uses `%-40s`. `fmt` pads by **runes**, not display cells, so names with CJK or emoji go out of line. Either reuse the TUI's `pad()` (`ansi.StringWidth`) or use `charm.land/lipgloss/v2/table` with `table.New().Border(lipgloss.HiddenBorder())`. The table measures cell width correctly and wraps or truncates by column. Output stays plain text when color is off.

### R27. Mouse (opt-in) — S–M

`v.MouseMode = tea.MouseModeCellMotion` in `View()`. Handle `tea.MouseWheelMsg` → `move(±3)` and `tea.MouseClickMsg` → `cursor = offset + (Y - listTop)`, plus a double click = use. Capturing the mouse breaks normal text selection in terminal emulators, and people copy host/port values, so make it **opt-in** (`--mouse` / `SNEAKERNET_MOUSE=1`) or mention "shift+drag to select" in help. The bare VT reports nothing without gpm, and turning it on does no harm there.

### R28. Golden render tests — M

`github.com/charmbracelet/x/exp/golden` is already in the module cache. Render `View().Content` (with `ansi.Strip` for readable goldens) for sizes {60×20, 80×24, 120×40} × {unicode, tty glyphs} × {search, add, logs, full help}. That catches the layout and glyph regressions R2/R19 could bring. Update with `go test ./internal/tui -update`.

### R29. cobra + fang — L

What it adds: subcommand help, `-h` anywhere, typo suggestions, `completion bash|zsh|fish`, a hidden `man` command (mango/roff), styled help and errors that downsample by color profile and honor `NO_COLOR` (via `colorprofile.NewWriter`), and `--version` [fang docs]. API: `fang.Execute(ctx, root, fang.WithVersion(version), fang.WithErrorHandler(...))`, import path `charm.land/fang/v2`.

Costs and caveats:
- A rewrite of `main.go`/`commands.go` dispatch and the `run()` test seam. Keep `run()` as a thin wrapper that calls `root.SetArgs/SetIn/SetOut/SetErr` so `main_test.go` still drives the real CLI.
- fang sets `SilenceErrors`. Its default error handler must keep the exit-code mapping (R13), so use a custom `WithErrorHandler` plus our own `os.Exit` codes.
- fang's styled help uses a palette chosen for dark truecolor terminals. Give it ANSI-index colors through `WithColorSchemeFunc` for the console.
- Binary size: a few hundred KB. That is minor next to Xray (~25 MB × 4 arches).

If cobra is not wanted: R17 + a hand-written completion script (≈40 lines of bash) give most of the value.

### R30. Offline completions — M (after R29)

At install time, generate the scripts with the installed binary (`sneakernet completion bash > …`) into `/usr/share/bash-completion/completions/sneakernet`, `/usr/share/zsh/site-functions/_sneakernet` and `/usr/share/fish/vendor_completions.d/sneakernet.fish`. Add the paths to `layout` and to the manifest so `uninstall` removes them. **Dynamic completion of server numbers** (`switch <TAB>` → `12  Berlin REALITY`) runs as the *user*, and `servers.txt` is 0600. It only works with the R24 non-secret index (index + name), so it has the same privacy trade-off.

### R31. FR-31 gaps — M

`--ui tui|gui|none` (today the installer always asks; `none` = headless) and `--dry-run` (print the plan: files, unit, user, ports, and exit 0). Both are in the PRD (US-8, FR-31). `--dry-run` also helps support: "run it with --dry-run and send me the output".

---

## 4. Libraries and techniques evaluated

| Library / tool | Verdict | Why |
|---|---|---|
| **bubbles `help`** (full mode, `KeyMap`, `SetEnabled`) | **Adopt** (R4, R5) | Already a dependency. Unused features cover discoverability. |
| **bubbles `progress`** | **Adopt** (R18) | Already a dependency. CP437-safe default glyphs. |
| **bubbles `spinner.Line`** | **Adopt** in tty mode (R2) | ASCII frames. |
| **bubbles `viewport` highlights** | **Adopt** (R23) | `SetHighlights` / `HighlightNext` exist in v2.2.1. |
| **lipgloss `table`** | Adopt for CLI tables (R26) | Measures display width. Borderless output is easy to pipe. |
| bubbles `list` / `table` for the TUI list | Skip | The custom search list with per-rune match highlighting is better than the built-in filter. |
| lipgloss `tree` | Optional | Could group `doctor` output (Files / Service / Network). Low value. |
| **cobra + fang** | Adopt later (R29) | Completions, man page, suggestions, styled help. Real migration cost. |
| **huh v2** (`charm.land/huh/v2`) | **Not now** | Its accessible mode (`Form.WithAccessible(true)`) prints numbered options and reads a number, which is what `ui.choose` already does [huh docs]. The interactive mode adds alt-screen-style redraws that work badly on serial consoles and screen readers. Revisit when the installer has real multi-field forms (sysproxy/TUN/user in M2–M3), and use it only when stdin/stdout are TTYs and `TERM != linux`. |
| glamour | Skip | Markdown rendering pulls in chroma/goldmark and a lot of binary weight for a help screen that §R4 covers. |
| harmonica | Skip | Spring animation. Already used inside `progress`. Animation adds noise on 9600-baud serial and VT consoles. |
| gum | Skip | Shell-script prompts. `install.sh` would have to ship gum for 4 arches. The Go binary already does this better. |
| charm `log` | Skip | `ui.step/ok/warn/fail` fits. The install log is plain text, which is what we want. |
| `x/exp/golden` | Adopt (R28) | Already in the mod cache. Golden render tests. |
| VHS | Optional | Scripted demo GIFs for the README. Maintainer-only, not shipped. |

### Lessons from reference TUIs

| TUI | Lesson | Applies as |
|---|---|---|
| lazygit | `?` shows *context-specific* keybindings; a bottom bar lists the most useful keys; confirmations are popups | R4, R5 |
| k9s | `:` command mode / palette; `?` help; custom hotkeys show up in help | R22, R4 |
| btop | Automatic **tty mode**: 16 colors, tty-safe symbols, forced with `-t` | R2 |
| htop / mc | F-key bar (`F1 Help … F10 Quit`), designed for the Linux console | R6 |
| gitui | Help popup, configurable keys | R4 (configurable keys: not needed yet) |
| superfile | Nerd-font icons are optional, with a plain fallback | R2 (no icon font assumptions) |
| soft-serve | The same app over SSH: no assumptions about the local terminal | R2, R14 |

### Accessibility notes

- Full-screen TUIs that redraw often work poorly with Speakup/BRLTTY on the console, and the redraws can spam speech output [xogium "text mode lie"]. Sneakernet already offers a linear CLI for every TUI action: `list`, `switch`, `test --all`, `add`, `remove`, `status`. **Document that** in the README and in the TUI's full help ("prefer a screen reader? use the commands: …"). Keep the installer line-based.
- The alt screen on the Linux VT: kernel support for `?1049h` arrived only recently (2025). The `linux` terminfo entry has historically had no `smcup`, and editors don't send it for `TERM=linux` [LKML vt smput patch]. On older live ISOs the TUI's last frame may stay on screen after quitting. To be safe, print a clear-screen on exit (or a one-line "Sneakernet closed — proxy is running/stopped") when `TERM=linux`. Test this.
- Don't rely on color alone (already true). Keep it that way when adding level colors to logs (R23): keep the `[Warning]` text.

---

## 5. Sources

- Command Line Interface Guidelines — https://clig.dev/ (output/stderr, `--json`/`--plain`, color and `NO_COLOR`, interactivity, confirmations, help, errors, exit codes)
- NO_COLOR convention — https://no-color.org/
- Charm huh v2 (accessible mode, v2 import path) — https://github.com/charmbracelet/huh/blob/main/UPGRADE_GUIDE_V2.md, Context7 `/charmbracelet/huh`
- Charm fang (`Execute`, `WithVersion`, `WithErrorHandler`, `WithColorSchemeFunc`, completions, man page) — https://github.com/charmbracelet/fang, Context7 `/charmbracelet/fang`
- Bubble Tea v2 `tea.View` (AltScreen, MouseMode, ProgressBar, Cursor), `WithColorProfile`, `RequestBackgroundColor` — local source `charm.land/bubbletea/v2@v2.0.10/tea.go`, `options.go`, `color.go`
- bubbles v2.2.1 `help` (styles, `ShortHelpView` skips disabled bindings), `spinner`, `progress`, `viewport` highlights, `key.SetEnabled` — local source `charm.land/bubbles/v2@v2.2.1`
- colorprofile env detection (`NO_COLOR` via `ParseBool`, `TERM` handling) — local source `github.com/charmbracelet/colorprofile@v0.4.3/env.go`
- btop man page (`--tty_on`: 16 colors, tty-friendly symbols) — https://manpages.opensuse.org/Leap-16.0/btop/btop.1.en.html
- Linux console glyph limits — https://en.wikipedia.org/wiki/Linux_console, https://lkml.iu.edu/1805.3/07370.html, https://bugzilla.kernel.org/show_bug.cgi?id=93241
- Linux VT alternate screen support (2025 patch, `TERM=linux` caveat) — https://lkml.iu.edu/2508.3/00243.html, https://upd.dev/kernel/linux/commit/23743ba64709
- TUI accessibility critique (Speakup/NVDA and redraw-heavy TUIs) — https://xogium.me/the-text-mode-lie-why-modern-tuis-are-a-nightmare-for-accessibility
- lazygit keybindings (`?` menu, context) — https://github.com/jesseduffield/lazygit/blob/master/docs/Custom_Command_Keybindings.md
- k9s hotkeys and command mode — https://k9scli.io/topics/hotkeys/
- setterm(1) (Linux console colors) — https://man7.org/linux/man-pages/man1/setterm.1.html

Not verified in this research (check before relying on it): exact glyph coverage of each distro's default console font (R2), the exact ANSI-16 mapping of the help greys (R3), alt-screen behavior on the specific live-ISO kernels (§4), and lessons for gitui/superfile/soft-serve, which come from general knowledge rather than fetched docs.
