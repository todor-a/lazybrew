# lazybrew Go rewrite specification

## 1. Status and requirement labels

This document is the authoritative behavior, architecture, security, and verification contract for replacing the completed Python/curses lazybrew with a Go/Bubble Tea implementation.

Requirements use these labels:

- **[CURRENT/PARITY]**: completed Python behavior that the rewrite must preserve.
- **[REWRITE ADDITION]**: behavior required in the Go rewrite but absent from the last completed Python state.
- **[IMPLEMENTATION]**: a required implementation constraint needed to make the observable or security contract reliable.

The last completed Python state had synchronous uninstall, inline confirmation, and no in-TUI administrator-password input. Later Python askpass, background-uninstall, and modal experiments are not current behavior and are not an implementation source. The Go rewrite must satisfy the additions in this document on its own design.

## 2. Product goal and non-goals

### Goal

lazybrew is a macOS terminal UI for inspecting installed Homebrew casks and explicitly requested formulae, viewing package information, and uninstalling one selected package safely. The Go rewrite must preserve the finished interaction model while replacing curses with Bubble Tea v2, Bubbles v2, and Lip Gloss v2. It must remain responsive while package information and uninstall commands run, and it must show administrator authentication inside a focused, masked dialog only when Homebrew invokes `SUDO_ASKPASS`.

### Non-goals

- Supporting Linux, Windows, package managers other than Homebrew, or remote Homebrew installations.
- Installing, upgrading, pinning, or otherwise mutating packages except uninstalling the explicitly confirmed package. **[REWRITE ADDITION]** Reporting which packages `brew outdated` names is a read and is not covered by this non-goal; the app surfaces the fact and takes no action on it. Section 9A designs an upgrade action but does not implement one, and this non-goal is amended only by the increment that implements it.
- Listing formulae installed only as dependencies.
- Pre-authenticating with `sudo -v` or prompting for a password before Homebrew asks for one.
- Caching or reusing administrator passwords.
- Parsing a pseudo-terminal, scraping a sudo prompt, using `sudo -S`, or suspending the TUI for terminal authentication.
- Adding Huh, another modal package, a shell wrapper, or a generated askpass script.
- Preserving Python implementation structure, curses rendering primitives, or Python tests.
- Generalizing command execution, process inspection, or terminal rendering for platforms other than macOS/Homebrew.

## 3. Domain model

### Package

**[CURRENT/PARITY]** A package is an immutable value with:

| Field | Contract |
|---|---|
| `name` | First non-whitespace token from one nonblank `brew list` output line. |
| `version` | The complete remainder of that trimmed line after the first token; it may be empty. |
| `kind` | Exactly `cask` or `formula`. No third value is valid. |
| `outdated` **[REWRITE ADDITION]** | Whether `brew outdated` reports the package for its kind. False when that read failed or was never made. |

Package identity and the package-info cache key are `(kind, name)`. Version and `outdated` are display data, not identity.

**[REWRITE ADDITION]** `outdated` replaces nothing; it is a fourth field on a value that previously carried three. It is display data specifically: it stays out of the info cache key, out of the search filter target, and out of every argv, so adding it cannot change which panel is cached, which rows a query matches, or what any command runs. It is set from the section 5 outdated read at list time and is never derived from comparing two version strings.

**[REWRITE ADDITION]** Before `brew info` or `brew uninstall`, reject an inventory value whose name is empty, begins with `-`, or contains NUL or a Unicode control character. An info rejection uses the cache/info-pane text `Unsafe package name; info refused`; an uninstall rejection reports `Unsafe package name; uninstall refused`. Either rejection starts no process. This guard keeps the parity argv forms below from interpreting an inventory value as an option.

### Active kind

**[CURRENT/PARITY]** Startup selects `cask`.

**[REWRITE ADDITION]** The header renders a tab bar naming both lists, not just the active one, and brackets the active one. The two exact labels are:

- `[ Apps ]    Formulae  ` for `cask`
- `  Apps    [ Formulae ]` for `formula`

Both labels are 22 cells wide. `Apps` occupies a fixed 8-cell slot at cells 1 through 8 and `Formulae` a fixed 12-cell slot at cells 11 through 22, with the bracket columns reserved on both sides, so switching swaps the brackets without shifting either name.

Brackets, not a `>` marker: `>` is what the package list uses for its selected row, and `Apps > Formulae` reads as a breadcrumb path rather than a choice between two tabs. Brackets need no color, so the cue survives monochrome themes and the header row keeps its single style, never nesting styles for the active tab.

The parity labels `Apps [casks]` and `Formulae [formula]` are replaced. They named only the active list, mismatched plurality between the two brackets, and paired a friendly name against a Homebrew term.

The list count is `<total> casks installed` or `<total> formulae installed`.

**[REWRITE ADDITION]** The count is computed at render time from the live list, not frozen into the event status slot when a list result lands. With a query active it reports the filtered figure instead: `<shown> of <total> casks match` or `<shown> of <total> formulae match`. An empty list reports no count at all, because the list pane's own `No packages found` already says that and a failed load would otherwise read as `0 casks installed`.

Parity stored the total in the status slot on list success, so it went stale two ways: a query typed after the load left the total unchanged, and a query carried across a kind switch reported the new list's unfiltered total beside a filtered pane. Both now follow the list.

A priority status still owns the whole row per composition rows 1 through 3, so a post-uninstall `Uninstalled <name>` shows no count until priority clears. That is unchanged and intentional.

**[REWRITE ADDITION]** The Homebrew plural is spelled `formulae` in every status string. Parity's `formulas` is replaced so the header, status, and row `kind` column no longer disagree.

## 4. Current feature set and exact keys

Key handling is mode-first. A modal mode receives ordinary keys before the underlying list. Mouse input is disabled. Any key not listed for the active mode is ignored, except the global interrupt contract in section 13.

### Normal mode

| Keys | Result | Label |
|---|---|---|
| `up`, `k` | Move selection up one visible row. Clamp at the first row; never wrap. | **[CURRENT/PARITY]** |
| `down`, `j` | Move selection down one visible row. Clamp at the last row; never wrap. | **[CURRENT/PARITY]** |
| `/`, `s`, `S` | Enter search-edit mode with the current query. | **[CURRENT/PARITY]** |
| `tab` | Switch cask ↔ formula, reset selection and scroll offset to zero, load that kind from the section 8 list cache or from `brew list` on a miss, then target its selected package for info. The query remains active, so the target list arrives already filtered. | **[CURRENT/PARITY]** plus cache **[REWRITE ADDITION]** |
| `u`, `U` | Open confirmation for the selected package if one exists and the safety-fit check passes. With no selected package, do nothing. | **[CURRENT/PARITY]** plus centered dialog **[REWRITE ADDITION]** |
| `t`, `T` | Cycle to the next theme and set status to `Theme: <name>`. | **[CURRENT/PARITY]** |
| `r`, `R` | Perform the refresh contract in section 8. | **[CURRENT/PARITY]** |
| `q`, `Q` | Cleanly quit with status 0. | **[CURRENT/PARITY]** |

No Bubbles default key that is absent from this table may remain enabled. In particular, paging, help expansion, fuzzy-search shortcuts, vim left/right paging, and default list quit bindings must not create additional behavior.

### Search-edit mode

Search is case-insensitive substring matching against `name + " " + version + " " + kind`. It preserves source list order; it does not fuzzy-rank results. The Go comparison is the substring relation after applying `strings.ToLower` to both operands.

| Keys | Result |
|---|---|
| Any printable text delivered by a `tea.KeyPressMsg` | Append the text to the query. `q`, `Q`, `u`, `t`, `r`, `s`, and `/` have no global meaning while editing. |
| `backspace`, `ctrl+h` | Remove the final query rune if one exists. Match a Bubble Tea v2 `KeyBackspace`/`backspace` event; also accept `ctrl+h` if an enhanced keyboard protocol delivers it distinctly. |
| `enter` | Accept the query, leave it active, and return to normal mode. |
| `tab` | **[REWRITE ADDITION]** Perform the normal-mode `tab` kind switch and return to normal mode. Parity ignored `tab` here, which made the advertised switch key silently dead while a query was being typed. Search mode is left before the load starts rather than when its result lands, so the mode change is visible immediately; the query is preserved and the target list arrives filtered. |
| `esc` | Clear the query, reset selection and scroll offset to zero, and return to normal mode. |

Each actual query edit resets selection and scroll offset to zero, clamps selection against the new filtered result, and retargets package info. Enter alone does not reset selection. All other non-printable keys are ignored.
Bubble Tea v2's default decoder maps both legacy BS (`0x08`) and DEL (`0x7f`) backspace bytes to `KeyBackspace`; this preserves the Python backspace-byte behavior. A physical forward-Delete key is the distinct `KeyDelete`/`delete` event and is ignored in search and password modes. This does not add a normal-mode Delete binding.

While editing, status is exactly `Search: <query>_`. Outside editing, the normal status prefix is `Search [/ or s]: <query-or-—>`.

### Confirmation mode

| Keys | Result |
|---|---|
| lowercase `y` only | Confirm the immutable package snapshot and start uninstall setup. |
| Every other ordinary key, including uppercase `Y`, `q`, `Q`, `enter`, and `esc` | Close the dialog, run no command, and set priority status to `Uninstall cancelled`. |

**[CURRENT/PARITY] Lowercase-only confirmation is mandatory. Uppercase `Y` must cancel.** Bubble Tea key handling must compare the exact key text to `y`; it must not use a case-insensitive binding.

### Uninstall-progress mode

The list is frozen. Ordinary keys, including `q`, `Q`, `u`, `r`, `tab`, and `esc`, are ignored both while uninstall setup is starting and while the uninstall child runs. The footer says `Uninstall in progress; controls disabled`. A verified askpass request temporarily places the model in password mode. `tea.WindowSizeMsg`, `ctrl+c`, and process signals always follow sections 6 and 13.

### Password mode

| Keys/messages | Result |
|---|---|
| Printable text in `tea.KeyPressMsg` | Append to the focused password input, subject to 256 runes and 1024 UTF-8 bytes. Bubbles `EchoPassword` renders `•` masks according to the value's terminal display-cell width, never the characters or a rune-count guarantee. |
| `backspace`, `ctrl+h` | Delete the final password rune. Apply the Bubble Tea v2 backspace distinction described above; physical `delete` is ignored. |
| `enter` | Submit the current value, including an empty value, to this askpass request; immediately reset and blur the input; return to uninstall-progress mode. |
| `esc` | Cancel the askpass request and the entire uninstall, wipe/reset/blur controlled password state, run the bounded cancellation and cleanup contract in section 11, and eventually show `Uninstall cancelled` unless cleanup fails. |
| `tea.PasteMsg` | Drop the message without passing its content to the widget or copying it into application state. |

All other keys are ignored. Disable the Bubbles textinput clipboard-paste binding, suggestions, completion, cursor movement, word editing, and clipboard access. lazybrew must never read from or write to the clipboard. Terminal-delivered printable `tea.KeyPressMsg` text remains ordinary input within the stated bounds.

The terminal-cell-width mask is a normative **[REWRITE ADDITION]** chosen to use the pinned Bubbles v2 `EchoPassword` behavior. It supersedes any pre-implementation draft that described one mask per rune; the completed Python app had no password renderer, so no shipped parity behavior changes.

## 5. Homebrew commands, parsing, and errors

### Exact command vectors

**[CURRENT/PARITY]** Every Homebrew operation uses an argv vector and no shell. The logical vectors are exactly:

| Operation | Kind | argv |
|---|---|---|
| List | cask | `brew`, `list`, `--cask`, `-1` |
| List | formula | `brew`, `list`, `--formula`, `--installed-on-request` |
| Info | cask | `brew`, `info`, `--cask`, `<name>` |
| Info | formula | `brew`, `info`, `--formula`, `<name>` |
| Uninstall | cask | `brew`, `uninstall`, `--cask`, `<confirmed-name>` |
| Uninstall | formula | `brew`, `uninstall`, `--formula`, `<confirmed-name>` |
| Dependents **[REWRITE ADDITION]** | formula | `brew`, `uses`, `--installed`, `<name>` |
| Outdated **[REWRITE ADDITION]** | cask | `brew`, `outdated`, `--cask`, `--quiet` |
| Outdated **[REWRITE ADDITION]** | formula | `brew`, `outdated`, `--formula`, `--quiet` |

**[REWRITE ADDITION]** The outdated vectors are new; they replace no earlier vector and change none. They carry no package value, only a kind, so the section 3 name validator has nothing to guard here. `--quiet` gives one bare name per nonblank line, parsed by the same parser as the dependents output. `--json=v2` is not used: names are all that is needed, and the JSON carries no installed size, so it would not earn a second parser.

`--greedy` must never be added. Homebrew's default set is already its own verdict about what `brew upgrade` would act on, and it deliberately excludes auto-updating casks it will not touch. Measured on the development machine: `brew outdated --cask --quiet` returns exactly `postman`, while adding `--greedy` returns 14 names including `firefox` — an `auto_updates` cask installed at 153.0.4 against Homebrew's 154.0 that `brew upgrade` will not touch. Marking firefox would be exactly the false claim the section 6 restraint forbids, so the flag is prohibited rather than merely unused.

**[REWRITE ADDITION]** Every command this adapter starts inherits `HOMEBREW_NO_AUTO_UPDATE=1`, appended to the environment rather than substituted for it.

`install`, `outdated`, `upgrade`, `bundle` and `release` are Homebrew's `AUTO_UPDATE_COMMANDS`. With `HOMEBREW_NO_AUTO_UPDATE` unset — the default — the first such command in each `HOMEBREW_AUTO_UPDATE_SECS` window runs `brew update --auto-update` before the command that was asked for: a network fetch that mutates the local Homebrew installation and its tap clones. Adding the outdated vectors brought the first such command into this application, and `list`, `info` and `uses` are not in that set, so nothing here triggered it before.

Two consequences make the suppression mandatory rather than an optimisation. These reads run inside the load that gates first paint, so a launch a day after the machine's last fetch would hold `Loading casks...` for as long as the network takes, with only supervised `q` accepted — unbounded, not merely slow. And the auto-update report is written to stdout in the same process that then execs the real command, so it lands in the buffer this adapter parses; a deleted-formula or deleted-cask line names an installed package by construction, and would be read back as inventory or as an outdated name.

It is applied to every read rather than only to the outdated vectors. Section 2 promises an application that mutates nothing except a doubly-confirmed uninstall, and updating the user's Homebrew as a side effect of drawing a list is outside that promise whichever read triggers it.

A test must assert the variable as the child received it, not as the parent set it. The development machine's shell exports `HOMEBREW_NO_AUTO_UPDATE=1`, which is precisely why this went unnoticed while the feature was built and measured.

The two kinds must never be collapsed into one `brew outdated --quiet` call. A formula and a cask can share a name, and the marker set is consulted per kind, so a combined call would let one inventory's outdated name mark the other inventory's row.

**[REWRITE ADDITION]** An absent marker asserts less than it appears to. `brew outdated --formula` also names the dependency-only formulae that the `--installed-on-request` inventory of section 3 never shows, and those names are discarded because no visible row carries them. A formula screen with no markers therefore means "none of the request-installed formulae shown here is outdated", never "nothing on this machine needs upgrading". The same holds when the outdated read fails: the list still loads, carrying no marks, which is indistinguishable on screen from a screen where nothing is outdated. Neither the row nor the status line claims otherwise, and no all-clear is stated anywhere.

The detail panel does distinguish the two, because it has room to. `Version` renders `(up to date)` only where a verdict was actually obtained; where the read failed and Homebrew's own text shows no newer version, the parenthetical is dropped and the installed version stands alone. This is the section 6 restraint about withheld evidence applied to freshness: a newer version parsed from `brew info` is independent evidence and still renders as `(latest <version>)` without a verdict.

The marker's meaning is delegated wholesale to Homebrew: "`brew outdated` reports this", which is also the precondition for upgrading it. This specification deliberately does not restate Homebrew's rule for which `auto_updates` casks land in the default set — `postman` does and `firefox` does not, and that rule could not be established from the receipts. The consequence is accepted: if Homebrew changes that default, the markers change with no code change here and no failing test.

**[REWRITE ADDITION]** There is no cask form of the dependents vector, and one must not be added. `brew uses` resolves its argument as a formula, so a cask token makes it warn about an unknown formula and report nothing; a cask must never be allowed to read as "nothing depends on this". `Uses` rejects a non-formula kind before starting any process, and applies the same package-name validator as `Info`, reporting `Unsafe package name; dependents refused`. Its output is one installed formula name per nonblank line.

The command adapter resolves `brew` through the child `PATH` without invoking a shell, then starts that executable with the remaining arguments exactly as listed. It must not concatenate a command string, invoke `sh -c`, add implicit flags, or substitute a package value not returned by the active inventory.

### List parsing

For each `strings.Split` logical line:

1. Trim surrounding whitespace.
2. Ignore the line if it is empty.
3. Split once at the first run of whitespace.
4. Store the first token as `name` and the entire trimmed remainder as `version`; use an empty version if there is no remainder.
5. Assign the requested kind to every row.

Preserve Homebrew output order.

### Command result mapping

All command stdout and stderr are captured separately.

1. If executable lookup or start fails because `brew` is absent, return `Homebrew is not installed or brew is not on PATH`.
2. For another lookup/start OS error, return `Could not run brew: <error>`.
3. For a nonzero exit, use trimmed stderr if nonempty; otherwise trimmed stdout if nonempty; otherwise `brew exited with status <code>`.
4. For success, return stdout. Package-info rendering removes trailing line terminators but preserves internal logical lines.

Single-line status fields flatten embedded command-output lines by joining logical lines with one ASCII space, then clip visually to their pane. The info viewport preserves logical lines and soft-wraps them.

A list-load error replaces that kind's list with an empty list and becomes status. An info error is the info text and is cached under the same generation rules as successful info. An uninstall failure becomes priority status and must not claim that the package list changed.

## 6. Bubble Tea UI, layout, and themes

### Bubble Tea primitives

**[IMPLEMENTATION]** The root model implements Bubble Tea v2 exactly:

```go
type Model interface {
    Init() tea.Cmd
    Update(tea.Msg) (tea.Model, tea.Cmd)
    View() tea.View
}
```

Use:

- Bubbles `list` for item selection, exact custom filtering, and list-compatible navigation.
- Bubbles `help` to render the mode-specific footer bindings.
- Bubbles `viewport` with `SoftWrap = true` for package info.
- Bubbles `spinner` while uninstall or a package-list reload is active.
- Bubbles `textinput` with `EchoPassword` for password entry. The search filter may use the list's filter input, but must satisfy the exact search contract above.
- Lip Gloss joins for the base layout and Lip Gloss v2 `Layer`/`Compositor` for centered overlays.

Do not use Huh or another modal package. `View` is pure: it renders state and must never cancel an operation, clear a modal, mutate selection, or perform I/O.

### Base layout

**[CURRENT/PARITY]** At 32×9 or larger, the screen contains:

1. A full outer border.
2. Header row: the section 3 list tab bar, alone. **[REWRITE ADDITION]** Parity rendered `lazybrew [<theme>]  <active-list>  Tab: switch`; all three parts around the tab bar are removed. The app name is redundant on a screen the user just launched. The theme name is permanent chrome for a value that only matters while cycling it, and `t` already reports `Theme: <name>` in the status row on demand. The `Tab: switch` hint named no destination; the tab bar itself does, and `tab switch` is now carried in the normal footer.
3. A horizontal rule.
4. A package content region of exactly `height - 7` rows.
5. A horizontal rule.
6. One status row.
7. One footer/help row.

The normal footer's exact logical string is `[/ or s] search  tab switch  u uninstall  t theme  r refresh  q quit`. Render that string through the mode keymap; Bubbles help must not substitute shorter or alternate labels. **[REWRITE ADDITION]** `tab switch` sits second, next to the other list-navigation key; parity omitted `tab` from the footer entirely and taught it only in the header.

Every footer is ANSI-aware clipped, never reworded, to the interior width. At the supported 32-column outer width, the interior is 30 cells and the normal footer content is the first 30 cells, exactly `[/ or s] search  tab switch  u`: cell 30 is the `u` of `u uninstall`. Structural view tests must compare the ANSI-stripped cell sequence and width, not merely search for help labels.

Package rows render a selection marker, a freshness cell, name, kind, and optional version. **[REWRITE ADDITION]** The row shape is ` <marker><freshness> <name-column> <kind>[ <version>]`, where the marker is `>` only for the selected row and `<freshness>` is one fixed cell holding `↑` for a package `brew outdated` reports and a space otherwise. This replaces the earlier ` <marker> <name-column> <kind>[ <version>]`, which had no cell for the outdated verdict. The name column is at least 8 and at most 30 cells when space permits. ANSI-aware clipping must keep every row inside its assigned pane and must never overwrite a divider or border.

**[REWRITE ADDITION]** The freshness cell sits immediately after the selection marker, so the narrow layout — where the list is the only pane and the marker matters most — cannot clip it away. It is taken from the name column, and only where the 30-cell cap is not already absorbing the slack: the name column arithmetic becomes `min(30, max(8, pane-width - 5 - len(kind)))`, one cell tighter than the previous `pane-width - 4 - len(kind)`. At 32 columns a cask row's name column goes from 22 to 21 cells and a formula row's from 19 to 18; at 72 columns from 27 to 26; at 80 columns and wider it stays 30, because the cap already discarded that cell. No pane, divider, border, or total row width changes.

Those counts are against the full row pane. A visible scrollbar takes one further cell from the same arithmetic, so with one present each count above drops by one — at 32 columns an unfiltered formula row's name column is 17, not 18. The pinned numbers therefore describe a list short enough or filtered enough to need no scrollbar; both values follow from the same expression, whose input is the row pane width rather than the list pane width.

The cell carries a glyph rather than a color, for the same reason the scrollbar thumb does: the cue must survive a monochrome theme. Because it lives inside the row string it inherits the selected-row style, including the monochrome reverse+bold, with no nested style. `↑` is an East-Asian-ambiguous-width character; the pinned Lip Gloss v2.0.6 measures it as one cell, and `•` and `█` are existing precedent for such a glyph here. A terminal configured to render ambiguous glyphs double-wide would shift every row by one cell at runtime — a terminal setting no test in this repository can observe. A unit assertion on the measured width therefore guards a dependency bump, not a terminal configuration.

The marker is not appended after the version: at 32 columns the pane is 30 cells and the row is already full at the kind column, so a trailing marker would be clipped exactly where the list is the only pane. Nor is it folded into the kind column as `cask*`: that would make the name column width differ per row, and the kind column would stop aligning within a pane — the same alignment reason the info panel's label width is fixed. Nor is it folded into the selection-marker cell, which cannot carry two independent bits without a cryptic third glyph.

#### List scrollbar

**[REWRITE ADDITION]** When the filtered list does not fit in one page, the list pane's final column is a scrollbar and rows are laid out in the remaining width. When it does fit, the column is absent and rows use the full pane width. Neither case changes the pane's total width, so the divider and border never move.

Only the thumb is drawn, as `█` in the border role; the track is blank. The column sits directly against the divider or the border, so a `│` track would render as `││` and read as a doubled border. A visible thumb is itself the signal that there is more list than fits, because the column only exists in that case. Thumb and track differ by glyph rather than by color, so the bar reads the same under a monochrome theme.

The list is paginated, not continuously scrolled, so the thumb is sized and positioned by page: its height is `max(1, rows / total-pages)` and its top is `(rows - thumb-height) * page / (total-pages - 1)`. Deriving the offset from travel and page index rather than from a proportion of rows makes the thumb sit flush against the top on the first page and flush against the bottom on the last, with no rounding gap that would suggest unseen content.

Total pages is derived from the rows currently visible and the page size, not read from the paginator's own counter, so a filter that has just shrunk the list cannot size the bar from stale bookkeeping.

Neither Bubbles nor Lipgloss provides a scrollbar widget. Bubbles' `paginator` renders only a page indicator (`1/4` or dots) and its `viewport` exposes scroll position without drawing it, so this column is drawn here rather than delegated. The list's own Bubbles pagination view stays disabled; it would be a second, weaker indicator of the same state.

If the filtered list is empty, render `No matching packages`; if the unfiltered list is empty, render `No packages found`. **[REWRITE ADDITION]** While a list command is active, render neither: the list pane's first row is blank, because the status row already owns the spinner and its `Loading ...`/`Refreshing ...`/`Reloading ...` text. Parity rendered `No packages found` next to `Loading formulae...` in the same frame, which read as the switch having emptied the list.

Selection never wraps. Keep the selected row inside the content region by adjusting the scroll offset. After filtering, list reload, kind switch, or refresh, both selection and offset must be valid even for an empty list.

### Responsive split

**[CURRENT/PARITY]** Below 72 columns, the package list receives the complete content width inside the border and no info pane or divider is rendered.

**[REWRITE ADDITION]** With no selected package the info pane renders its own first row as `Loading info...` while a list command is active, and stays fully blank otherwise. A list load clears the selection, so the pane has no package to head and no details to fetch; parity left it blank for the entire load, which on a cold kind switch is the whole several-hundred-millisecond window, with only the status row showing that anything was happening. An empty list that is not loading stays blank, because the list pane's own `No packages found` already covers it. The string is the same `Loading info...` the info loader uses for a pending fetch, shared as one exported constant so the two cannot drift.

At 72 columns or wider, place the divider at integer `width / 2`. The list occupies the cells inside the left border and before the divider. The right pane begins after the divider and renders:

- `Info: <selected-name>` on its first content row.
- The selected package's info beneath it in a soft-wrapped viewport, clipped to remaining content rows.

#### Info pane content

**[REWRITE ADDITION]** Parity printed `brew info` verbatim. That output is written for someone about to install a package — provenance, build options, download analytics — while this screen serves someone deciding whether to remove one they already have. The pane therefore renders a curated panel: the one-line description, a blank row, then aligned label/value rows, and for a formula a blank row and a removal verdict.

Rows appear only when their value is available, in this order:

| Row | Value |
|---|---|
| `Version` **[REWRITE ADDITION]** | Installed version, then exactly one of: `(outdated, latest <version>)` when `brew outdated` reports the package and a distinct newer version parses; `(outdated)` when it reports the package and no distinct newer version parses; `(latest <version>)` when it does not report the package but the parsed versions differ; `(up to date)` otherwise. |
| `Size` | Installed size, then file count when Homebrew reports one. |
| `Installed` | `as a dependency`, and only then. |
| `License` | Homebrew's license field. Formulae only; casks do not carry one. |
| `Home` | Homepage URL. |
| `Needed by` | `nothing installed`, or up to three dependent names followed by `and <n> more`. Formulae only. |

Labels are padded to a fixed width equal to the longest label, not to the longest label present in a given panel, so the value column lands on the same cell for every package and does not shift as the selection moves.

The verdict row is `Safe to remove.` when a formula has no installed dependents, or `Removing this breaks <n> installed formula(e).` when it does.

Three deliberate restraints, because this pane feeds a destructive action:

1. **[REWRITE ADDITION]** A version is labelled `outdated` only on Homebrew's own `brew outdated` verdict, never from a version mismatch. This replaces the earlier absolute "a version is never labelled `outdated`", which withheld a fact Homebrew is willing to state, because the only source available at the time was a version comparison the pane was right not to trust. The reason for that distrust is unchanged and now pinned by example: firefox is an `auto_updates` cask installed at 153.0.4 while Homebrew offers 154.0, and `brew outdated --cask` does not report it, so its row carries no marker and its panel still reads `Version    153.0.4  (latest 154.0)` — a version mismatch is still reported with no conclusion drawn. The normative consequence is that `(up to date)` must never render for a package `brew outdated` reports, and `outdated` must never render for one it does not.
2. `Needed by` and the verdict require a dependent lookup that actually succeeded. A cask has no such lookup, and a formula whose lookup failed gets neither row: absence of evidence must never render as an assurance of safety.
3. A failed dependent lookup does not fail the load. The panel renders without those two rows, since details that did load are more useful than an error for a package whose details are fine.

The fields are parsed from the same `brew info` text, not from `brew info --json=v2`, because the JSON carries no installed size and size is the field this screen exists for. Any row that cannot be parsed is omitted, and when neither a description nor a size can be found the raw text is rendered unchanged — a Homebrew output change degrades to the parity behaviour rather than to a blank pane.

**[REWRITE ADDITION]** The one field not parsed from that text is the outdated verdict. It arrives on the section 3 package value from the section 5 outdated read, so exactly one component decides the word and the panel cannot form a second opinion from the two version strings it already parses. Those strings remain display detail; the verdict is the fact. The list row and this panel therefore render one value from one source and cannot disagree.

On a new info target, set viewport Y offset to zero. There are no info-scrolling keys; the viewport supplies width-aware wrapping and clipping, not an additional navigation feature. Info loading continues and caches results even while the terminal is too narrow to show the pane.

### Status precedence

Render the first applicable state:

1. Password/confirmation/uninstall priority state.
2. Search-edit state: `Search: <query>_`.
3. Other priority error or success status.
4. Normal search prefix plus ordinary status: `Search [/ or s]: <query-or-—> | <status>`.
5. Normal search prefix alone.

`<query-or-—>` uses an em dash for an empty query.

**[REWRITE ADDITION]** The normal search prefix carries the section 3 list count as its own ` | `-joined segment, between the query and any ordinary status, and is omitted when the list is empty. Rows 4 and 5 above therefore render as `Search [/ or s]: <query-or-—> | <count> | <status>` and `Search [/ or s]: <query-or-—> | <count>`. Rows 1 through 3 are unchanged and carry no count.

**[REWRITE ADDITION]** The status prefix gains no outdated segment. It stays exactly `Search [/ or s]: <query-or-—> | <count> | <status>` with three segments. The row markers already carry the number, and a count there would be a second, weaker statement of the same fact — the same reason the list's own Bubbles pagination view stays disabled.

### Themes

**[CURRENT/PARITY]** Cycle in this exact order: Lazygit (default) → Bright → Ocean → Dracula → Lazygit.

“Default” means leave that Lip Gloss color unset. ANSI color numbers are normative where a color is named.

| Theme | Border fg/bg | Header fg/bg | Selected fg/bg | Status fg/bg | Footer fg/bg | Search fg/bg |
|---|---|---|---|---|---|---|
| Lazygit | green (2) / default | green (2) / default | default / blue (4), not bold | default / default | blue (4) / default | cyan (6) / default |
| Bright | cyan (6) / black (0) | black (0) / cyan (6) | black (0) / yellow (3) | green (2) / black (0) | black (0) / white (7) | cyan (6) / black (0) |
| Ocean | cyan (6) / black (0) | white (7) / blue (4) | black (0) / cyan (6) | white (7) / blue (4) | cyan (6) / black (0) | cyan (6) / black (0) |
| Dracula | magenta (5) / black (0) | white (7) / magenta (5) | black (0) / magenta (5) | cyan (6) / black (0) | yellow (3) / black (0) | cyan (6) / black (0) |

Search-edit mode applies the search role to both the outer border and search prompt. On a monochrome/no-color profile, the UI remains usable and the selected row uses reverse plus bold. Lazygit's selected row must not become bold when color is available.

### Centered overlays

**[REWRITE ADDITION]** Render confirmation and password dialogs as bordered Lip Gloss layers centered over the complete base screen. The base screen remains visible underneath. The modal layer has a higher Z value and receives all ordinary input.

The confirmation dialog contains exactly:

- Title: `Confirm uninstall`
- Body: `Uninstall <snapshot-name>?`
- Help: `y: confirm  other: cancel`

The password dialog contains:

- Title: `Administrator password`
- Body on the first request: `Homebrew requested administrator access.`
- Body on a second or later request: `Wrong password? Try again.`
- A focused field labeled `Password: ` using the mask glyph `•`
- Help: `Enter: submit  Esc: cancel`

No helper-provided prompt, argv text, socket text, or other untrusted string is rendered in either dialog.

### Size safety

**[CURRENT/PARITY]** Minimum usable size is 32 columns × 9 rows. Below it, render only `lazybrew: terminal too small (need 32x9)`. Ignore non-quit input; `q` and `Q` still quit.

Before opening confirmation, construct and ANSI-measure the largest of the confirmation dialog, password dialog including retry text, `Uninstalling <name>...`, `Cancelling <name>...`, and the list-reload statuses in section 8. Include border and horizontal padding. If any required line or dialog exceeds the current terminal, run no command and set `Widen terminal to confirm`.

A resize re-runs this safety check:

- If confirmation is open and no longer fits, close it and set `Terminal too small; uninstall cancelled`.
- If uninstall/password handling is active and safe progress/authentication UI no longer fits, fail closed: cancel the askpass request, run the bounded cancellation and cleanup contract in section 11, wipe controlled password state, and retain `Terminal too small; uninstall cancelled` for display when the terminal is usable again unless cleanup fails.

The fit transition occurs in `Update` when handling `tea.WindowSizeMsg`, never as a side effect of `View`.

## 7. Asynchronous latest-only package info

**[CURRENT/PARITY]** Selection and query changes never wait for `brew info`.

The latest-only info loader owns these values behind its interface:

- current generation, starting at 0;
- current selected key or none;
- current display text;
- cache keyed by `(kind, name)`;
- at most one active request identity `(generation, key)`;
- at most one pending request, replaced by newer targets.

The exact transition rules are:

1. Selecting no package clears the display target and pending request. It does not cancel the active command.
2. Selecting a cached key renders it immediately and clears pending.
3. Selecting the active request's identity displays `Loading info...`, clears the pending request, and does not enqueue a duplicate.
4. Selecting an uncached, inactive key displays `Loading info...`. If no request is active, start it immediately; otherwise replace the single pending request.
5. Rapid navigation therefore runs at most one `brew info` process and retains only the latest pending target.
6. A completed request may populate the current generation's cache even when no longer selected.
7. A completed request renders only when its key equals both the loader's current target and the model's currently selected package key.
8. The active identity remains active until its result message is handled. Revisiting a process that has completed but whose message has not yet been handled must not start a duplicate.
9. After handling an active result, start the one retained pending request if it is still uncached and different from the new active state.
10. A result from an older generation is discarded: it must not render and must not enter the current cache.

Bubble Tea command completion messages wake `Update` and redraw without a keypress; the Python 100 ms polling loop is deleted rather than reproduced. Spinner ticks may redraw only while a spinner is active. When no spinner or command is active, the model returns no timer/tick command; handling an unexpected stale tick neither changes state nor schedules another tick.

On shutdown, the supervised quitting contract in section 13 cancels any active info context and waits for its result/reap message.

## 8. Refresh and list reload

### Refresh

**[CURRENT/PARITY]** `r` or `R` performs one atomic logical refresh:

1. Increment the info generation.
2. Clear the entire info cache, current info key/text, and pending info request. **[REWRITE ADDITION]** Clear the retained per-kind list cache in the same step; see the list cache contract below.
3. Start reloading the active cask/formula list without a shell.
4. Keep the previous numerical selection while loading, then clamp it and its scroll offset to the refreshed, filtered list.
5. On list success, clear the status slot and schedule fresh info for the resulting selected package. **[REWRITE ADDITION]** The count is no longer written here; the section 6 status prefix derives it from the refreshed list, so the rendered row is unchanged for an unfiltered load and correct for a filtered one.
6. On list failure, replace the list with empty data, show the mapped error, and leave no info target.

An old-generation info process may finish, but its result follows the stale-result rule and cannot repopulate the cleared cache.

### Kind switch, startup loading, and exact loading modes

Homebrew I/O runs in `tea.Cmd`, never inside `Update`. Startup initializes the cask model and starts its list command from `Init`. A kind switch resets selection/offset and starts the target kind's list command. There is at most one list command at a time. Each list operation owns a retained context/cancel function until its typed result message has been handled; the command adapter has then returned from `Run`/`Wait` and reaped both of its directly owned children.

**[REWRITE ADDITION]** That one list command performs two concurrent Homebrew reads for the same kind — the section 5 list vector and the section 5 outdated vector — and returns one typed result message carrying packages already marked. "At most one list command at a time" is preserved literally: one command, one retained context, one cancel function, one completion handle, one message, one cache entry, one invalidation pair. No second command, context, message type, or supervisor slot exists for the outdated read. The two reads must write two distinct locals joined before either is read; neither may touch model state, which the Tea loop mutates concurrently.

There is deliberately no second, independently-landing outdated command. It would duplicate the list command's set/clear and quit-guard bookkeeping, and — decisively — the info fetch and the outdated fetch would race, so the list row and the detail panel would visibly disagree about one fact for several hundred milliseconds.

The measured cost is a startup and load regression, and it is accepted: a load now takes `max(list, outdated)` rather than `list`. On the development machine `brew list --cask -1` is about 30 ms and `brew outdated --cask --quiet` about 550 ms, so a cold cask start goes from roughly 30 ms to roughly 550 ms; a cold formula load goes from roughly 400 ms to roughly 600 ms. The existing spinner and `Loading ...` status already own that window, and every other Homebrew read in this app is already a half-second read.

Those figures hold only because the adapter suppresses Homebrew's auto-update; see section 5. They were first measured on a shell that exported `HOMEBREW_NO_AUTO_UPDATE=1`, which made the suppressed case look like the default one. Without the suppression the same read is unbounded rather than slow, so the number above is a cost only under the guarantee that section 5 now makes explicit.

### List cache

**[REWRITE ADDITION]** Every successful list result is retained per kind. A kind switch to a retained kind is served from that retention: it starts no command, enters no loading state, and renders the target list in the same frame as the key press. Only a kind not retained this session starts a `loadSwitch` command. Parity re-ran `brew list` on every switch, so each one cost a fresh empty pane.

Map presence is the retention test, not list length. A kind with nothing installed is retained as an empty list and is a hit, rather than re-shelling on every switch.

Retention is dropped wholesale — both kinds — at exactly the two sites that already drop the info cache, and only there:

1. `r` or `R` refresh, per the section 8 refresh contract. Refresh means the inventory may have changed outside the app, so no kind stays trusted.
2. A committed uninstall, before its `loadAfterUninstall` reload starts. Uninstalling one kind can change the other, so both are dropped rather than reasoning about which.

The info cache and the list cache are invalidated together at both sites and must stay that way: anything that can change what `brew info` prints can change what `brew list` prints. After either site the reload repopulates only the active kind, so the next switch is a miss and re-lists.

Retention is per session and is not revalidated. An install or uninstall performed in another terminal is not observed until `r`. Parity's per-switch re-list masked that, at the cost of the empty pane on every switch; `r` is the explicit remedy.

A failed list result caches nothing, so a broken Homebrew never poisons retention. The other kind's earlier retention does survive that failure and can still be switched to.

**[REWRITE ADDITION]** The retained per-kind list carries its outdated marks. A cached kind switch therefore renders them in the same frame as the key press and still starts no command; the marks are part of the retained value, never re-fetched.

A failed outdated read is absorbed, exactly as a failed dependent lookup is. The list result is still a success, is still retained, and simply carries no marks; the detail panel keeps its unmarked wording. A broken outdated read must never poison retention, fail the load, or render as an assurance that nothing is outdated.

The marks are per session and are not revalidated, on the same terms as the list itself: an upgrade performed in another terminal is not observed until `r`, which is the explicit remedy. Because the marks are baked into the formatted text the info cache holds, the info cache and the list cache must continue to be dropped together at every invalidation site; a site that dropped one but not the other would let a stale marker disagree with a stale panel.

The key, priority-status, and footer contracts while a list command is active are exact:

| Load state | Priority status with active spinner | Footer | Ordinary keys |
|---|---|---|---|
| Startup cask load | `Loading casks...` | `q quit` | `q` or `Q` enters supervised quitting; every other ordinary key is ignored. |
| Kind-switch load | `Loading casks...` or `Loading formulae...` | `q quit` | `q` or `Q` enters supervised quitting; every other ordinary key is ignored. |
| Cached kind switch | none; no command runs | `[/ or s] search  tab switch  u uninstall  t theme  r refresh  q quit` | **[REWRITE ADDITION]** No load state is entered, so normal mode keeps every ordinary key. |
| User refresh | `Refreshing casks...` or `Refreshing formulae...` | `q quit` | `q` or `Q` enters supervised quitting; every other ordinary key is ignored. |
| Post-uninstall reload | `Reloading casks...` or `Reloading formulae...` | `Uninstall in progress; controls disabled` | Every ordinary key, including `q` and `Q`, is ignored because destructive transaction completion is pending. |

`tea.WindowSizeMsg` and the global interrupt contract are handled in every row. Startup, switch, and refresh return to normal mode on their list result. Post-uninstall reload remains uninstall-progress mode with the list frozen and controls disabled until its result establishes final success or failure.

## 9. Uninstall, progress, and result states

### Immutable confirmation

**[CURRENT/PARITY]** Pressing `u` or `U` copies the selected package's name, version, and kind into an immutable confirmation snapshot. The dialog and eventual argv use this snapshot. Later filtering, list results, resize, or selection state must never alter the target.

Confirmation state is cleared before the starting-uninstall transition is committed. A repeated key cannot confirm twice. Only one uninstall job may exist.

### Start transaction

**[REWRITE ADDITION]** Lowercase `y` performs this nonblocking transition:

1. Revalidate the immutable snapshot and size safety.
2. Clear confirmation, create and retain the operation context/cancel function, commit `starting-uninstall` state with priority status `Uninstalling <name>...`, and activate the spinner.
3. Return a non-secret `tea.Cmd` that calls synchronous `Uninstaller.Start(context, snapshot)`. `Start` must never execute inside `Update`.
4. Inside `Start`, create and validate the private askpass directory and listener, start accepting only after the listener is bound and ready, construct the canonical per-child environment, use the brew preparation seam in section 12 with that environment, and only then start the exact uninstall argv in its dedicated tracked process group.
5. The command returns `jobStartedMsg{job}` only after the job owns the started child and broker resources, or `jobStartFailedMsg{err}` only after every partially created owned resource has been cleaned. Neither message contains a secret.
6. Handling `jobStartedMsg` retains uninstall-progress state and starts the job event/result commands. Handling `jobStartFailedMsg` stops the spinner, clears the operation, and returns to normal mode with the mapped failure.

Starting state handles resize and global interruption and ignores ordinary keys exactly like uninstall-progress mode. Cancellation may therefore latch through the retained context before or during `Start`; if `Start` races and still returns a job, the handler immediately cancels it and awaits its one terminal result.

A broker/directory/listener setup failure reports `Could not start uninstall: <error>`. A brew preparation/lookup/start failure uses the exact command error mapping in section 5 without adding that prefix. No half-created broker remains.

### Progress

The starting-uninstall state is committed and renderable before the returned command can call `Start`; the active spinner and `Uninstalling <name>...` are present in that committed model. This is an ordering guarantee about model state, not an impossible guarantee that a physical terminal paint occurs before Bubble Tea launches the command goroutine. The list remains frozen and the footer says `Uninstall in progress; controls disabled`.

### Completion

- On uninstall command failure, complete the bounded cleanup contract, then show the command error as priority status. Keep the existing list; do not report success. A cleanup failure instead reports `Uninstall cleanup failed: <error>`.
- On password cancellation, authentication timeout, terminal safety cancellation, or global interruption, complete bounded cleanup before reporting the outcome. User cancellation reports `Uninstall cancelled`; authentication/protocol rejection reports `Administrator authentication failed`; timeout reports `Administrator authentication timed out`. A cleanup failure takes precedence and reports `Uninstall cleanup failed: <error>`.
- On uninstall command success, do not yet show success. Enter the exact post-uninstall reload state in section 8. If reload succeeds, set `Uninstalled <snapshot-name>`, clamp selection/offset, and schedule fresh selected-package info. If reload fails, show the reload error and do not show `Uninstalled`. A cleanup failure can never be reported as success.

**[CURRENT/PARITY]** Success is observable only after both `brew uninstall` and the active list reload succeed.

## 9A. Upgrade — designed, not implemented

**[REWRITE ADDITION]** This section is a design, not a shipped contract. No code implements it. Nothing in sections 1 through 9 or 10 through 17 describes upgrade behaviour, the `g` key does not exist, the footer string is unchanged, and section 2's non-goal against mutating packages other than by uninstalling still holds exactly as written. That non-goal is amended only by the increment that implements this section; until then it is correct as it stands.

It is written down for one reason: `brew upgrade` on a cask can require administrator authentication exactly as `brew uninstall` can, so an upgrade action needs the whole `internal/uninstall` machinery — private askpass endpoint, kernel peer authentication, tracked process group, bounded cleanup, immutable confirmation snapshot. That machinery must be generalised, never forked, copied, or bypassed. A second copy of the security path would be a worse outcome than no upgrade action at all. The generalisation below was checked against the implementation; it is small, and the reason for deferring it is entirely in `internal/ui`.

### What is already operation-agnostic

The security core carries nothing uninstall-specific: the endpoint, the framed protocol, peer authentication, process-group ownership, bounded cleanup, and the `Job` contract are all indifferent to which brew verb runs. The single seam that produces the argv is already a package-level test seam in `internal/uninstall`. Generalisation is therefore a signature change plus a rename, not a redesign of anything in sections 10, 11, or 13.

### `internal/brew`: one more verb through the same seam

`PrepareUninstall(env, pkg)` becomes `PrepareCommand(env, op, pkg)` with:

```go
type Operation uint8

const (
    Uninstall Operation = iota
    Upgrade
)

func PrepareCommand(env []string, op Operation, pkg Package) (ResolvedCommand, error)
```

Two argv rows join the section 5 table:

| Operation | Kind | argv |
|---|---|---|
| Upgrade | cask | `brew`, `upgrade`, `--cask`, `<confirmed-name>` |
| Upgrade | formula | `brew`, `upgrade`, `--formula`, `<confirmed-name>` |

The section 12 rule is restated **stronger**, not weakened: no second package validator, **command** argv builder, executable resolver, or command-failure mapper may exist. Dropping the word "uninstall" widens the rule's scope while keeping it singular — one validator, one resolver, one argv builder, one failure mapper, now covering two verbs. The unsafe-name rejection text becomes per-operation, `Unsafe package name; uninstall refused` or `Unsafe package name; upgrade refused`, and still starts no process.

### `internal/uninstall` → `internal/privileged`

The package is renamed, because a package named for one verb cannot honestly own two:

- `internal/uninstall` → `internal/privileged`; the section 12 planned-file list becomes `internal/privileged/privileged.go`, `internal/privileged/protocol.go`, `internal/privileged/peer_darwin.go`, `internal/privileged/*_test.go`, replacing the four `internal/uninstall/*` entries.
- `Uninstaller` → `Runner`; `Start(ctx, pkg)` → `Start(ctx, brew.Operation, brew.Package)`, with the operation passed straight through to the existing prepare seam.
- Three error strings lose the verb: `Could not start uninstall: %w` → `Could not start %s: %w`; `fatal uninstall cleanup failure` → `fatal cleanup failure`; `uninstall workers did not stop before cleanup deadline` → `workers did not stop before cleanup deadline`.
- Nothing else in the package changes. `Job`, `Event`, `Result`, `RequestID`, the helper dispatch, and every invariant in sections 10, 11, and 13 are untouched.

The rename and the signature change must land in the **same** increment, so no half-generalised state ships. The rename touches every file of a security-critical package, which makes silently dropping a test file or one of its package-level `var` test seams the highest-consequence mistake available in this repository: a seam that stops being overridden still compiles, still vets, and still passes. The increment must therefore verify, file by file, that every test file and every seam survives with the same override sites.

### `internal/ui`: the actual cost

The model gains the operation alongside its existing snapshot, and about ten SPEC-pinned strings become per-operation:

| Uninstall (pinned today) | Upgrade (to be pinned) |
|---|---|
| `Confirm uninstall` | `Confirm upgrade` |
| `Uninstall <name>?` | `Upgrade <name>?` |
| `Uninstalling <name>...` | `Upgrading <name>...` |
| `Uninstall in progress;` / `controls disabled` | `Upgrade in progress;` / `controls disabled` |
| `Uninstalled <name>` | `Upgraded <name>` |
| `Uninstall cancelled` | `Upgrade cancelled` |
| `Uninstall cleanup failed: <error>` | `Upgrade cleanup failed: <error>` |
| `Terminal too small; uninstall cancelled` | `Terminal too small; upgrade cancelled` |
| `Could not start uninstall` | `Could not start upgrade` |

`Cancelling <name>...`, `Widen terminal to confirm`, `Administrator authentication failed`, and `Administrator authentication timed out` are shared and unchanged. The lowercase-`y`-only confirmation discipline is reused verbatim and must never be re-derived. The fit check that guards the confirmation and password dialogs must measure the longest of **both** verb sets, so a terminal wide enough to confirm one verb cannot be too narrow to render the other mid-operation.

Key `g` starts an upgrade. The footer becomes `[/ or s] search  tab switch  u uninstall  g upgrade  t theme  r refresh  q quit`, with `g upgrade` immediately after `u uninstall`. That changes the section 6 exact 30-cell prefix at width 32 only if the new key is inserted before cell 30; it is not — `g upgrade` sits after `u uninstall`, so the 30-cell prefix `[/ or s] search  tab switch  u` is unchanged.

`g` on a package that `brew outdated` does not report starts nothing at all: no confirmation, no snapshot, no job. It sets the non-priority status `<name> is up to date`. The destructive machinery stays unreachable for an operation that would be a no-op, and the freshness cell from section 6 is exactly the affordance that tells the user which rows `g` will act on.

### The third cache-invalidation site

A committed upgrade reloads the active list exactly as a committed uninstall does, and must drop both caches. Section 8's "at exactly the two sites" therefore becomes three sites when this section is implemented; the sentence must be amended in the same increment rather than left stale. The pairing rule is unchanged and is what makes the third site safe: an upgrade changes both what `brew list` prints and what `brew info` prints, and the section 6 outdated marks are baked into the cached panel text, so dropping one cache without the other would leave a stale `↑` beside a fresh panel.

### Why this is deferred rather than shipped

The `internal/brew` and `internal/privileged` work above is cheap and low-risk. Shipping only that would leave a generalised seam with a single caller — a speculative abstraction by this repository's own rules, and therefore worse than shipping neither half.

The cost is the `internal/ui` half: a near-clone of the confirmation → start → progress → password → completion → reload state machine, every string of which is pinned by this document, so each new string must be invented, reviewed, and pinned; plus the fit check re-measured across both verb sets; plus the third invalidation site; plus a roughly doubled section 9/10/11 verification matrix; plus re-pointing the capability-gated real-`sudo` positive test at `brew upgrade`, which — unlike `brew uninstall` against a fixture tree — mutates a package the test cannot restore. Item 1 of this feature, shipped and driven, plus this design is the better trade than a rushed clone of a security-adjacent state machine.

## 10. On-demand administrator password and retry

### Invocation rule

**[REWRITE ADDITION]** Do not open a password dialog when uninstall starts. Open it only after Homebrew invokes the configured askpass helper and the broker authenticates that helper connection.

Homebrew currently adds `sudo -A` when `SUDO_ASKPASS` exists. lazybrew must not invoke sudo itself, preflight credentials, or infer that a package will need administrator access.

### Self-helper dispatch

The Go binary doubles as the askpass helper. Program startup order is:

1. Inspect the raw environment. If any `LAZYBREW_ASKPASS_MODE` occurrence is present, enter helper dispatch before TTY checks and Bubble Tea startup, then validate the complete helper metadata; duplicate or malformed metadata exits silently with status 1.
2. In helper dispatch, set `RLIMIT_CORE` soft and hard limits to zero before connecting to the socket or reading any response. Failure exits silently with status 1.
3. Otherwise, in normal dispatch set `RLIMIT_CORE` soft and hard limits to zero before TTY checks and before Bubble Tea can accept any password. Record failure as an authentication capability failure: browsing may continue, but an authenticated uninstall request is canceled before password mode with `Administrator authentication failed`.
4. Continue normal lazybrew startup.

Construct the uninstall child's environment from a private copy by removing every occurrence of `SUDO_ASKPASS`, `LAZYBREW_ASKPASS_MODE`, and `LAZYBREW_ASKPASS_SOCKET`, then add exactly one canonical value for each:

- `SUDO_ASKPASS=<absolute, symlink-resolved path of the running lazybrew executable>`
- `LAZYBREW_ASKPASS_MODE=1`
- `LAZYBREW_ASKPASS_SOCKET=<absolute retained private socket path>`

Never append these keys to possibly duplicated inherited entries and never mutate the process-global environment. Helper metadata is well formed only when each key occurs exactly once, mode is exactly `1`, `SUDO_ASKPASS` is the absolute resolved path of the loaded helper executable, and the socket is an absolute path named `askpass.sock` under a single private directory rooted at the resolved literal `/tmp` and within the Darwin path limit. These checks reject malformed routing data but do not replace kernel peer authentication.

These values are routing metadata, not authentication secrets. Never include a password, password hash, reusable token, prompt response, or request result in environment or argv.

Helper mode ignores sudo's prompt argv and emits no logs or diagnostics. On a password response it writes exactly the password bytes followed by one newline to stdout and exits 0. On cancel, malformed metadata/protocol, core-limit failure, timeout, EOF, or any error it writes zero bytes to stdout and stderr and exits 1.

### Retry

Each sudo askpass invocation creates a new connection, new random request ID, new focused textinput value, and new mutable password byte buffer. After a submitted password, close and discard that request. If sudo invokes askpass again, show a fresh dialog with `Wrong password? Try again.` No password value survives or pre-fills a retry. lazybrew does not count or limit retries beyond sudo's behavior.

## 11. Askpass protocol and security invariants

### Private endpoint

**[IMPLEMENTATION]** For each uninstall job:

- Resolve the literal macOS path `/tmp` with symlinks and require its real target to be an existing directory. Do not use `TMPDIR`, `os.TempDir`, an environment-selected root, or an unresolved alias.
- Under that retained real root, create `lazybrew-<cryptographically-random-suffix>` with an atomic mode-0700 directory create. Retain the exact returned path; collision retries use a new random suffix.
- Verify with `lstat` that the retained created path is a directory, is owned by the current effective UID, has permission bits exactly 0700, and is not a symlink.
- Bind only the fixed child `askpass.sock`, set permission bits exactly 0600, and verify with `lstat` that the retained path is a socket owned by the current effective UID, has permission bits exactly 0600, and is not a symlink.
- Reject before bind if the UTF-8 path plus its terminating NUL does not fit Darwin `sockaddr_un.sun_path` (104 bytes, so the path is at most 103 bytes).
- Use listener backlog 1. Permit one live request at a time. A concurrent second request is a protocol failure and cancels the job.
- Cleanup unlinks only the retained fixed socket path and removes only the retained private directory. Never recursively delete or use a path received from environment or protocol.

The listener must be bound, permission/type/owner reverified, accepting, and reported ready before the brew child starts. Any resolution, random creation, chmod, lstat, bind, listen/accept-readiness, environment construction, brew preparation, or child-start failure cleans the exact resources already created; no later step and no brew child starts after an earlier setup failure.

### Framed protocol

Use an AF_UNIX `SOCK_STREAM` connection with exact reads/writes. Every frame has this 28-byte, network-byte-order header:

| Offset | Size | Value |
|---|---:|---|
| 0 | 4 | ASCII magic `LBAP` |
| 4 | 1 | Protocol version `1` |
| 5 | 1 | Message type |
| 6 | 2 | Reserved; must be zero |
| 8 | 16 | Request ID from `crypto/rand` |
| 24 | 4 | Unsigned payload byte length |

Message types are:

| Value | Direction | Payload |
|---:|---|---|
| 1 `REQUEST` | helper → broker | Length must be 0. |
| 2 `PASSWORD` | broker → helper | 0–1024 UTF-8 bytes; the newline is not part of the frame. |
| 3 `CANCEL` | broker → helper | Length must be 0. |
| 4 `ERROR` | broker → helper | Length must be 0. |

The response must repeat the request ID. A completely framed connection carries exactly one request and one terminal response. After exact-writing the single `REQUEST` frame, the helper must call `shutdown(SHUT_WR)` (`CloseWrite`) and continue reading the response half. The broker exact-reads the request frame and then requires EOF on the request half within the protocol deadline before authenticating the peer or emitting `PasswordRequested`; any additional byte or missing EOF is a protocol failure. A nonblocking peek is not proof of request completion.

After a completely framed request, the broker exact-writes one terminal response and closes the connection. An authenticated request ends with `PASSWORD`, `CANCEL`, or `ERROR`; an authentication rejection writes `ERROR` when the framed connection remains writable, then closes, and never writes `PASSWORD`. A request that fails before complete framing may be closed without a response. The helper exact-reads one response frame and requires EOF before emitting password bytes. Reject bad magic/version/reserved bytes, unknown type, wrong direction, mismatched ID, oversized length, short read, bytes after either single frame, missing required EOF, or unexpected EOF. A request ID correlates frames; it is not accepted as authentication.

Use a 2-second connect deadline and a 2-second deadline for each protocol read/write and required request/response EOF outside user entry. The helper may wait up to 5 minutes for the terminal response after its authenticated request; expiry sends no password, cancels the uninstall, and reports `Administrator authentication timed out`.

### Kernel peer authentication

A private pathname is not authentication against another process running as the same user. At normal startup, capture the resolved executable path and the running process's Darwin Security.framework dynamic-code identity, including the loaded main code's CDHash. Before emitting a password-request event or opening the dialog, the broker must fail closed unless all checks pass on the still-open connection:

1. Darwin kernel socket credentials report the current effective UID.
2. Darwin `LOCAL_PEERPID` reports a live peer PID.
3. The peer's kernel-reported loaded main-executable path equals the startup resolved path configured as `SUDO_ASKPASS`.
4. Security.framework dynamic-code inspection of that live PID reports the loaded peer code identity/CDHash, and it exactly matches the running normal process identity/CDHash captured at startup. A `stat`, vnode identity, hash, or signature lookup of the current pathname is not a substitute: path replacement and pathname swap-back after peer `exec` must not authenticate a different loaded image.
5. The peer's immediate live parent is `/usr/bin/sudo`, and that process argv contains the askpass flag `-A` as a distinct argument.
6. Walking live parent PIDs reaches the exact tracked brew child PID before PID 1, a loop, a vanished process, or 64 ancestors.
7. The peer and sudo remain in the tracked uninstall process group.

Obtain UID/PID, loaded-executable path/code identity, and ancestry from Darwin kernel process facilities and Security.framework. A small macOS-only cgo/syscall implementation for dynamic-code identity is permitted and adds no third-party Go module. Never trust a UID, PID, parent PID, executable path, code identity, process group, or nonce supplied by the frame or environment. Keep the connection open throughout verification and response. If any evidence or API is unavailable, ambiguous, races with process exit, or fails, send no password, send framed `ERROR` only when the complete connection remains writable, close the connection, cancel the job, and complete bounded cleanup with `Administrator authentication failed` unless cleanup itself fails.

### Password handling

The following invariants are release blockers:

- Password characters are displayed only as `•`; the real value never enters rendered content. The visible mask length, defined by terminal display-cell width, is the only permitted derived value in the rendered UI.
- Password material unavoidably exists transiently in `tea.KeyPressMsg.Text`, Bubbles' private rune storage, the short-lived immutable string returned by `Value()`, controlled mutable byte buffers, and kernel/runtime copies. It must never be retained, formatted, debug-dumped, logged, traced, queued beyond the active input transition, or copied into status/errors.
- `EchoPassword` is display masking only. On submit, convert `Value()` directly to a bounded byte slice, immediately `Reset()` and `Blur()` and replace/discard the textinput state, drop the immutable string reference before returning from the transition, write the frame synchronously to the authenticated local connection with the bounded write deadline, and overwrite the mutable slice before returning.
- Never put an assembled password or any derived value other than the permitted mask length in a custom application `tea.Msg`, `tea.Cmd` closure, application channel/queue, cache, status, error, log, trace, metric, argv, environment, file, socket pathname, clipboard, or captured process result.
- Never generically format or dump a `tea.Msg`, the model, protocol frame, child environment, or process result while password mode or askpass request handling is active. No debug/error path may bypass this prohibition.
- Drop `tea.PasteMsg` entirely in password mode without routing or copying `Content`; keep Bubbles clipboard paste unbound and suggestions/history/completion disabled.
- The helper receives into a bounded mutable byte buffer, validates the frame without converting the payload to a string, writes payload bytes plus newline directly to stdout, and overwrites all mutable frame/payload buffers in `defer` paths.
- Reset, blur, replace, and discard the widget on submit, cancel, retry, timeout, peer EOF, protocol error, resize cancellation, interrupt, and normal job completion.
- Normal and helper dispatch independently establish `RLIMIT_CORE=0` at the points in section 10; neither assumes inheritance or the other process's success.
- Go strings, `tea.KeyPressMsg.Text`, widget-private rune storage, kernel buffers, and runtime copies cannot be guaranteed to be zeroized. Memory erasure is explicitly best-effort; minimize every lifetime and overwrite every mutable buffer controlled by lazybrew.

The only custom application message queued for a password request contains non-secret request identity and a response handle. Submitting the password calls that handle directly during the active update transition; it does not enqueue the password.

### Cancellation and process ownership

The following single cleanup definition applies everywhere this document says cancel, clean, or reap an uninstall:

1. Start brew in a dedicated process group and retain its direct-child PID, process-group ID, context cancel function, known authenticated descendant PIDs, and exactly-one `Wait` ownership.
2. Cancellation latches permanently. Reject later helper requests without a dialog; send `CANCEL` to an active authenticated request when possible, close its connection, wipe controlled UI/helper buffers, and close the listener.
3. Cancel the context, send `SIGTERM` to the tracked process group, and boundedly observe the direct child, known descendants, and group for up to 2 seconds. Then send `SIGKILL` to the group if any observable member remains and boundedly observe again.
4. Exactly one owner calls `Wait` exactly once to reap the directly owned brew child. lazybrew joins every goroutine and closes every listener/connection it owns. Non-child sudo/helper/privileged descendants are reaped by their own parent or the OS; lazybrew must never claim to reap them.
5. If a known privileged or escaped descendant survives, group inspection/signalling is permission-denied or otherwise ambiguous, the direct child cannot be confirmed reaped, or an owned goroutine/descriptor cannot be joined/closed within the bound, return explicit cleanup failure. Never report ordinary cancellation, authentication failure, or successful complete cleanup over that failure.
6. Emit exactly one terminal uninstall result after cleanup. Resource cleanup remains idempotent, exact-path-only, and bounded even after partial setup.

The one job-result command owns `Wait`; quitting cancels the job and awaits that existing terminal result rather than calling `Wait` again. No uninstall or broker goroutine may be abandoned as a daemon.

## 12. Deep modules, interfaces, and concrete package plan

The rewrite uses four deep modules. Their small interfaces are the seams crossed by callers and tests; security and orchestration complexity remains inside their implementations.

### Planned files

```text
lazybrew/
  go.mod
  go.sum
  cmd/lazybrew/main.go
  internal/brew/brew.go
  internal/brew/exec.go
  internal/info/loader.go
  internal/ui/model.go
  internal/ui/view.go
  internal/ui/keys.go
  internal/ui/theme.go
  internal/uninstall/uninstall.go
  internal/uninstall/protocol.go
  internal/uninstall/peer_darwin.go
  internal/brew/*_test.go
  internal/info/*_test.go
  internal/ui/*_test.go
  internal/uninstall/*_test.go
```

No non-Darwin peer adapter or generic package-manager layer is planned.

### Bubble Tea UI module: `internal/ui`

Interface:

```go
func New(homebrew Homebrew, info *info.Loader, uninstaller Uninstaller) (tea.Model, *Supervisor)
func (s *Supervisor) Cleanup(context.Context) error
```

`cmd/lazybrew/main.go` retains the returned supervisor before calling `tea.NewProgram(...).Run()`. The model and every list/info/uninstall operation register their contexts and completion handles with that same supervisor. `Cleanup` is idempotent, applies the bounded cancellation/await contract in section 13, and is the documented post-`Run` startup/runtime-error fallback; main never has to reach into UI or adapter internals.

The module's external interface is Bubble Tea's `Init`/`Update`/`View`. Its implementation owns modes, keys, list model, viewport, spinner, textinput, themes, geometry, status precedence, immutable confirmation, and translation of adapter/job results into state. It does not know command argv construction, socket framing, peer authentication, or process cleanup.

Tests send typed Bubble Tea messages through `Update` and inspect `View`/resulting model state using fake adapters. They do not call rendering internals.

### Homebrew command adapter: `internal/brew`

Interface consumed by UI/info:

```go
type Homebrew interface {
    List(context.Context, Kind) ([]Package, error)
    Outdated(context.Context, Kind) ([]string, error) // [REWRITE ADDITION]
    Info(context.Context, Package) (string, error)
    Uses(context.Context, Package) ([]string, error) // [REWRITE ADDITION]
}

type ResolvedCommand struct {
    Path string
    Args []string
}

func PrepareUninstall(env []string, pkg Package) (ResolvedCommand, error)
func MapCommandFailure(runErr error, stdout, stderr []byte) error
```

`PrepareUninstall` is the narrow uninstall boundary: it applies the same package-name validator used by `Info`, resolves `brew` through `PATH` in the supplied canonical child environment, and returns only the resolved executable plus arguments exactly equal to the uninstall table after `brew`. `MapCommandFailure` implements the single lookup/start/nonzero-exit mapping in section 5; it returns nil for a nil run error. The real adapter and uninstall module both use these functions. No second package validator, uninstall argv builder, executable resolver, or command-failure mapper may exist.

**[REWRITE ADDITION]** `Outdated` is a plain read on the same seam: it reuses the one kind-flag table, the one `run` helper, the one name parser, and `MapCommandFailure`, and adds no validator because it passes no package value. A missing `brew` reported through `Outdated` must produce the same exact text as through `List`, `Info`, or `PrepareUninstall`.

The real adapter hides process execution/reaping, stdout/stderr capture, list parsing, and UI-facing calls. `internal/uninstall` alone adds the canonical askpass environment, process-group ownership, broker, and cleanup to the prepared command; the brew seam does not expose arbitrary commands. A fake adapter supplies deterministic lists/info/errors in model and loader tests.

### Latest-only info loader: `internal/info`

Interface:

```go
type LoadFunc func(context.Context, brew.Package) (string, error)

func New(load LoadFunc) *Loader
func (l *Loader) Select(*brew.Package) tea.Cmd
func (l *Loader) Refresh(*brew.Package) tea.Cmd
func (l *Loader) Complete(Result) tea.Cmd
func (l *Loader) Text() string
```

`Result` contains generation, package key, text/error, and no secret. The loader implementation owns generation, cache, current target, active identity, latest pending target, and stale-result rules. `Select`, `Refresh`, and `Complete` are the only mutation seam; callers cannot manipulate cache or active/pending state. Loader tests use a controlled fake `LoadFunc` and complete requests out of order.

### Uninstall/askpass module: `internal/uninstall`

Interfaces:

```go
type Uninstaller interface {
    // Start performs synchronous setup/start and is called only inside a tea.Cmd.
    Start(context.Context, brew.Package) (Job, error)
}

type Job interface {
    Events() <-chan Event
    RespondPassword(RequestID, []byte) error
    CancelPassword(RequestID) error
    Cancel()
    Wait() Result
}
```

`Events` carries only `PasswordRequested` metadata and closes when the job terminates; the terminal result comes exclusively from the single `Wait` call owned by one command/supervisor. No event contains a password. `RespondPassword` takes ownership of the mutable byte slice for the duration of the call, writes it directly to the authenticated connection, wipes it before returning, and never retains or queues it. `Cancel` is idempotent; `Wait` returns only after the direct brew child has been `Wait`-reaped once, owned resources have been closed/joined, bounded descendant observation has completed, and any cleanup failure is present in `Result`.

The implementation hides self-helper metadata, directory/socket setup, framing, deadlines, Darwin peer/ancestry/code-identity checks, process-group signalling and observation, retry connections, buffer wiping, and cleanup. It is constructed with and exclusively uses `brew.PrepareUninstall` and `brew.MapCommandFailure`; it cannot duplicate their validation, argv, resolution, or error mapping. The UI knows none of those details. Fake jobs drive password-request, retry, cancel, success, cleanup-failure, and command-error transitions without invoking sudo. Security integration tests cross the same interface with real local processes and a controlled fake brew tree.

This division is deliberately not a set of pass-through wrappers. Deleting any module would spread its state machine or platform/security rules across callers, demonstrating its depth and the locality it provides.

## 13. Lifecycle, exit, and resize

### Normal process

**[CURRENT/PARITY]** stdin and stdout must both be TTYs. Normal mode checks this after helper-mode dispatch. If either is not a TTY, write `lazybrew requires an interactive terminal` to stderr and exit 1.

Start Bubble Tea in the alternate screen. Hide the cursor except when the focused password input requires Bubble Tea's cursor representation; restore terminal state on every exit. No mouse mode is enabled.

If Bubble Tea startup or runtime fails, execute the bounded post-`Run` fallback below, write `lazybrew could not start: <error>` to stderr using the joined runtime/cleanup error, and exit 1. Normal supervised `q`/`Q` exits 0 only after successful cleanup; cleanup failure exits 1. `ctrl+c` or SIGINT supervises cleanup, restores the terminal, and exits 130; SIGTERM performs the same cleanup and exits 143. A cleanup failure is never converted to a successful exit.

In normal/search/confirmation mode, `q` behavior follows the mode tables. During starting-uninstall, active uninstall, and post-uninstall reload, ordinary `q` is ignored so transaction ownership cannot be abandoned. A global interrupt has priority over every mode and enters the tracked cancellation path.

### Supervised quitting

Normal-mode `q`/`Q`, and `q`/`Q` during startup, kind-switch, or refresh loading, enter a `quitting` state rather than returning `tea.Quit` immediately. Its priority status is exactly `Quitting...`; its footer is `Cleanup in progress; controls disabled`; ordinary keys are ignored and resize/global signals remain handled.

The operation supervisor:

1. cancels retained list and info contexts;
2. awaits their already-running typed result messages, which prove their adapters returned from `Run`/`Wait` and reaped directly owned children, while suppressing those canceled results from visible list/info state;
3. cancels any uninstall job and awaits its existing one-owner terminal result; if `Uninstaller.Start` is still in flight, cancels its context, awaits `jobStartedMsg` or `jobStartFailedMsg`, and immediately cancels/awaits a raced successful job;
4. returns `tea.Quit` only after all applicable completion/reap messages have been handled.

The contexts, job registration, and completion handles live in an operation supervisor that outlives the Bubble Tea run loop. If `Run` returns a startup/runtime error before `Update` can finish quitting, the caller invokes the same cancellations and boundedly awaits those completion handles before restoring the terminal and returning. The fallback uses the command adapters' finite kill/`WaitDelay` bounds and the uninstall cleanup bounds; timeout or uninspectable survivors join an explicit cleanup failure to the runtime error. It never polls on a periodic UI tick.

### Helper process

Helper dispatch occurs before TTY validation because askpass stdout is a pipe to sudo. Helper exits 0 only after writing one valid password response plus newline. It exits 1 silently for cancel or failure.

### Resize

`tea.WindowSizeMsg` updates root width/height, list size, content-row capacity, pane widths, viewport width/height, help width, and modal safety in one `Update` transition. Initial and later window-size messages are sufficient; do not poll dimensions. Every resize redraws even when no other state changed.

## 14. Dependencies and versions

Use Go `1.27` and pin these direct modules exactly:

```go
require (
    charm.land/bubbletea/v2 v2.0.9
    charm.land/bubbles/v2   v2.2.0
    charm.land/lipgloss/v2  v2.0.6
)
```

Use only the Go standard library beyond these direct dependencies. Do not mix v1 `github.com/charmbracelet/...` imports with v2 imports. Do not add Huh, a fuzzy-search library, wrapping library, spinner library, password widget, modal library, command runner, logging framework, or cross-platform process library.

## 15. Clean-cutover deletion map

After the Go implementation and replacement tests pass, delete rather than retain or port:

- `lazybrew/lazybrew.py`
- `lazybrew/test_lazybrew.py`
- `lazybrew/__pycache__/`
- every `.pyc` under `lazybrew/`

Do not carry forward:

- the Python shebang, runtime, `curses.wrapper`, `Lazybrew` screen controller, `addstr`, manual curses borders/dividers/colors, ACS handling, or `textwrap` layout;
- Python `NamedTuple`, `threading.Condition`, `queue.Queue`, daemon info worker, `unittest`, or mocks;
- any generated Python askpass helper/script, Python interpreter isolation design, ctypes/socket/struct experiment, old askpass constants, Python password buffer, Python background-uninstall/result queue, or their cancelled tests.

The cutover is complete: no Python compatibility shim, alias, duplicate executable, deprecated path, or retained Python test remains. Preserve the contracts in this document, not the old implementation shape.

## 16. Acceptance criteria

Parity and rewrite are complete only when all of the following are true:

1. Startup selects casks and uses the exact list commands and parser.
2. Formula listing excludes dependency-only formulae through `--installed-on-request`.
3. Every mode accepts exactly the keys specified here; confirmation accepts lowercase `y` only and uppercase `Y` cancels; list-loading and post-uninstall reload use their exact key/status/footer tables.
4. Search is case-insensitive substring matching over name, version, and kind, preserves source order, and has the specified accept/cancel/reset behavior, including Bubble Tea v2 backspace versus forward-Delete semantics.
5. Selection and scrolling never wrap or leave valid bounds after filtering, reload, refresh, kind switch, or an empty result.
6. All Homebrew commands use exact argv with no shell, share the one validator/preparation/error-mapping seam, and map all lookup/start/exit errors as specified.
7. Narrow/wide layout, info wrapping, ANSI-aware clipping, exact logical status/footer strings, themes, monochrome selection, and minimum-size behavior match this document.
8. Info loading is nonblocking, single-active/latest-pending, clears pending on active-target reselect, redraws on completion without a keypress, and cannot render stale selection or pre-refresh data.
9. Refresh clears the complete info generation and cache, reloads the active list, clamps selection, and schedules fresh info.
10. Uninstall cannot start without a selected inventory package, a fitting terminal, and exact lowercase-`y` confirmation of an immutable snapshot.
11. The confirmation dialog is centered over the base UI, and command target cannot change after the dialog opens.
12. Starting-uninstall state and spinner are committed/renderable before the nonblocking `Start` command is invoked; `Start` never runs in `Update`, and the UI remains responsive to resize/authentication/interrupt with ordinary controls disabled.
13. Successful uninstall is reported only after command success and successful active-list reload; failures never falsely change the list or claim success.
14. The password dialog appears only after a kernel-authenticated Homebrew/sudo askpass connection, is centered/focused/masked by terminal cell width, supports enter/backspace/escape exactly, drops `tea.PasteMsg`, and creates a fresh empty input on each retry.
15. Helper mode starts before Bubble Tea/TTY checks, independently establishes `RLIMIT_CORE=0`, validates canonical metadata, and produces exactly password-plus-newline on success or silent nonzero failure on cancel/error.
16. The real `/tmp` endpoint, canonical child environment, half-closed framed stream, deadlines, peer UID/PID/loaded-path/dynamic-code-identity/parent/ancestry/group verification, fail-closed behavior, and no-secret rules pass their specified security tests.
17. Except for unavoidable transient framework/widget/runtime copies and the permitted mask length, password material never enters retained application messages/queues, command closures, cache, status, errors, logs, traces, argv, environment, files, clipboard, or captured command results; every controlled mutable buffer is wiped on every terminal path.
18. Cancel, resize failure, interrupt, quit, and runtime fallback `Wait`-reap the directly owned brew/list/info children exactly once, join/close owned goroutines/descriptors, boundedly signal and observe known uninstall descendants, and report explicit cleanup failure for survivors or uninspectable state.
19. The four module seams are tested through fake adapters/jobs, and real integration tests cross the same interfaces rather than reaching into implementation internals.
20. Python source, tests, bytecode, and cancelled askpass experiments are deleted in the same clean cutover.
21. **[REWRITE ADDITION]** Outdated marking is sourced only from `brew outdated` per kind, never with `--greedy` and never from a version comparison; appears identically in the list row's freshness cell and the info panel's `Version` row; rides the retained list across a cached kind switch without starting a command; is wholly absent when the outdated read fails, without failing or unretaining the load; and never renders `(up to date)` for a package Homebrew reports as outdated.

## 17. Verification matrix

| Area | Required verification | Observable pass condition |
|---|---|---|
| Command vectors | Table-driven unit tests for both kinds and list/info/uninstall/outdated, including unsafe names. **[REWRITE ADDITION]** The outdated cases assert both vectors and that `--greedy` never appears, and that an invalid kind starts no process. | Exact argv equality; shell path is never used; unsafe info/uninstall text is exact and no process starts; the outdated vectors are exactly `outdated --cask --quiet` and `outdated --formula --quiet`. |
| Parsing/errors | Unit tests for blank lines, empty/multiple versions, missing brew, generic spawn error, stderr/stdout/fallback exit errors, and shared preparation/mapping calls. | Results and strings exactly match sections 3 and 5; uninstall has no duplicate validator, argv builder, resolver, or failure mapper. |
| Keys/modes | Bubble Tea model tests send every listed key/message plus representative unlisted keys in every mode, including `KeyBackspace`, separately delivered `ctrl+h`, physical `KeyDelete`, and `tea.PasteMsg`. | Exact transition tables; `y` confirms, `Y` cancels, search/password steal global letters, loading modes supervise `q`, progress/reload ignore controls, physical Delete and password paste are ignored. |
| Filtering/selection | Model tests edit/accept/cancel queries, switch kinds, reload shorter/empty lists, and resize viewports. | Source-order substring results and all indices/offsets remain valid. |
| Layout | Golden or structural view tests at 31×8, 32×9, 71×N, 72×N, narrow names, long names, long status, and multiline info. **[REWRITE ADDITION]** Plus: an outdated row renders ` >↑ ` and a fresh row ` >  ` at identical total width; the freshness cell is independent of the selection marker; and the measured width of `↑` is asserted to be one cell. | Minimum-size message, split threshold, clipping, wrapping, status, and no border/divider overwrite; at width 32 the ANSI-stripped normal footer is exactly the 30-cell prefix specified in section 6; every row is exactly the terminal width at 32, 72, and 120 columns with marked and unmarked rows both on screen, and the section 6 name-column counts hold. |
| Themes | View tests for cycle order and every role; no-color rendering test. | Exact table values; Lazygit selected row not bold with color; reverse+bold without color. |
| Latest-only info | Controlled fake load function completes active/pending requests in adversarial order, including completed-not-handled revisit, refresh during flight, and A active → B pending → A reselected. | One active/one latest pending; current-generation cache only; stale result never renders/caches; after A completes in the A→B→A case, B never starts. |
| Refresh/list loading | Model plus fake Homebrew tests cover every section 8 row, success/error, filtered query, fewer rows, and in-flight old info. **[REWRITE ADDITION]** Plus: marks survive a cached kind switch with no extra list call; a failed outdated read yields a successful, retained, unmarked list; an outdated name the inventory never shows marks nothing. | Exact status/footer/key behavior, cache invalidation, list/status result, selection clamp, and fresh info request; marks are on the retained value; a failed outdated read changes neither status nor retention. |
| Confirmation | Model/view tests for no selection, immutable snapshot, all ordinary keys, long package names, and shrink while open. | No command without fitting exact lowercase `y`; centered dialog; safe cancellation strings. |
| Uninstall start/progress/result | Run the `tea.Cmd` returned by a fake model transition for setup success/failure, command failure, success+reload success/failure, cancel, cleanup failure, and duplicate events. The fake `Start` asserts starting state and spinner were committed before invocation and that `Start` was not called by `Update`. | One nonblocking start transition and one job/result; listener/setup precedes child start; no premature success; exact progress/reload/final status and cleanup-failure precedence. |
| Askpass helper/core limit | Subprocess tests invoke both normal and helper dispatch with inspectable/injected limit setters, duplicate/malformed metadata cases, and a test broker. | Both paths establish `RLIMIT_CORE=0` before their specified boundaries; failure refuses password input/connect; duplicate or malformed metadata is rejected silently; no TUI/TTY check runs in helper mode; exact stdout newline appears only on success; retry starts empty. |
| Private endpoint and child environment | Darwin integration/fault-injection tests use the real resolved literal `/tmp`; hostile pre-existing/duplicate askpass variables; symlink/type/owner/mode/path-length failures; and failure at every resolution/create/chmod/lstat/bind/listen/readiness/environment/prepare/start step. | Random directory is exactly 0700, socket exactly 0600, retained types/ownership are correct, symlinks and overlong paths fail, canonical child env contains exactly one value per key while the parent env is unchanged, listener is ready before child start, no child starts after setup failure, and only retained exact paths are removed. |
| Protocol | Unit/fuzz tests cover every frame field, 0/1024/1025-byte payloads, short/partial I/O, wrong IDs, missing request half-close, bytes after request, missing response EOF, bytes after response, EOF, and deadlines. | Helper `CloseWrite`, broker request EOF, single terminal response, and helper response EOF are required; malformed/oversized/trailing input fails closed without password output. |
| Peer verifier logic | Pure verifier tests use controlled process/code-identity snapshots for valid and invalid UID, PID, loaded path/CDHash, parent, `-A`, group, ancestry, vanished-process, loop, and ambiguity cases. | Logic accepts only a complete internally consistent trusted snapshot; these tests do not claim to exercise kernel acquisition. |
| Darwin peer acquisition | Darwin integration tests acquire real kernel socket UID/PID, loaded executable/parent/argv/group data, and Security.framework dynamic loaded-code identity from controlled processes; include same-UID direct connector, path replacement, and pathname swap-back after malicious peer `exec`. | Acquired fields equal independently expected controlled-process values; current-path `stat` cannot make replacement/swap-back match the retained lazybrew identity; unavailable, inconsistent, or ambiguous acquisition fails closed. These tests do not claim a positive complete `/usr/bin/sudo -A` ancestry. |
| Secret exclusion | Instrument fake log/status/event/result/env/argv and generic message/model/frame debug sinks; scan temp paths/captured outputs; exercise key input, dropped paste, submit, retry, escape, timeout, EOF, resize, SIGINT, and start failure. | No retained password or derived value appears except the permitted mask length; widget is reset/blurred/replaced, immutable references are dropped promptly, and controlled buffers are overwritten. |
| Process cleanup | Integration-test a fake brew tree that ignores SIGTERM, holds descriptors, survives where controllable, and simulates permission-denied/uninspectable members. | Direct brew child is `Wait`-reaped exactly once; SIGTERM then bounded SIGKILL/observation; fixture descendants are observed exited; owned goroutines/FDS close; exact paths are removed; survivor/uninspectable cases return explicit cleanup failure; cleanup is idempotent. |
| TTY/lifecycle | PTY and non-PTY subprocess tests cover normal quit, runtime failure fallback, ctrl+c/SIGINT, SIGTERM, and quit during active list, info, starting uninstall, and active uninstall. | Exact stderr and exit codes 0, 1, 130, or 143; terminal restored; list/info result-reap messages and uninstall terminal result are awaited; no owned command/goroutine/descriptor is abandoned; cleanup failures are surfaced. |
| Quiescence | Model/runtime test reaches idle after list/info/uninstall completion and then observes commands/redraw triggers. | No 100 ms or other polling timer exists; with no active spinner/command, no tick is scheduled, and a stale tick neither mutates state nor schedules another. |
| End-to-end parity | PTY-drive the built binary with a fake `brew` on `PATH` for cask/formula browsing, search, theme, refresh, info, confirm/cancel, uninstall failure, and uninstall success. | All visible strings, commands, transitions, and reload semantics satisfy this spec. |
| End-to-end authentication | Keep three boundaries distinct: pure verifier fixtures as above; mandatory Darwin kernel/Security.framework integration with controlled processes; and a capability-gated PTY test in which fake Homebrew invokes real `/usr/bin/sudo -A` only when the host permits non-destructive sudo askpass testing. | Pure fixtures prove only verifier logic, Darwin integration proves kernel acquisition/fail-closed behavior, and the real-sudo positive path proves on-demand dialog/masking/fresh retry/submission when capability is available; skipping that capability-gated positive test does not replace either mandatory lower layer. |
| Clean cutover | Repository check after implementation. | Go tests pass and no Python source/test/bytecode/helper artifact remains under `lazybrew/`. |

No destructive verification may uninstall a real user package. End-to-end command tests must put a controlled fake `brew` first on an isolated `PATH` and assert captured argv/environment metadata without recording password bytes.

## 18. References

### Current behavior sources

- Existing implementation: `lazybrew/lazybrew.py`
- Existing behavioral tests: `lazybrew/test_lazybrew.py`
- Homebrew `SUDO_ASKPASS` → `sudo -A` command construction: https://github.com/Homebrew/brew/blob/master/Library/Homebrew/system_command.rb

### Bubble Tea v2 stack

- Bubble Tea v2.0.9 release: https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.9
- Bubble Tea v2 model, command, view, and runtime source: https://github.com/charmbracelet/bubbletea/blob/v2.0.9/tea.go
- Bubble Tea v2 migration guide: https://github.com/charmbracelet/bubbletea/blob/v2.0.9/UPGRADE_GUIDE_V2.md
- Bubble Tea screen/window sizing: https://github.com/charmbracelet/bubbletea/blob/v2.0.9/screen.go
- Bubble Tea commands: https://github.com/charmbracelet/bubbletea/blob/v2.0.9/commands.go
- Bubble Tea process execution semantics: https://github.com/charmbracelet/bubbletea/blob/v2.0.9/exec.go
- Bubbles v2.2.0 release: https://github.com/charmbracelet/bubbles/releases/tag/v2.2.0
- Bubbles v2 migration/import guide: https://github.com/charmbracelet/bubbles/blob/v2.2.0/UPGRADE_GUIDE_V2.md
- Bubbles list/filter source: https://github.com/charmbracelet/bubbles/blob/v2.2.0/list/list.go
- Bubbles list key map: https://github.com/charmbracelet/bubbles/blob/v2.2.0/list/keys.go
- Bubbles help: https://github.com/charmbracelet/bubbles/blob/v2.2.0/help/help.go
- Bubbles viewport: https://github.com/charmbracelet/bubbles/blob/v2.2.0/viewport/viewport.go
- Bubbles spinner: https://github.com/charmbracelet/bubbles/blob/v2.2.0/spinner/spinner.go
- Bubbles password textinput: https://github.com/charmbracelet/bubbles/blob/v2.2.0/textinput/textinput.go
- Lip Gloss v2.0.6 release: https://github.com/charmbracelet/lipgloss/releases/tag/v2.0.6
- Lip Gloss layers/compositor: https://github.com/charmbracelet/lipgloss/blob/v2.0.6/layer.go
- Lip Gloss joining: https://github.com/charmbracelet/lipgloss/blob/v2.0.6/join.go
- Lip Gloss placement: https://github.com/charmbracelet/lipgloss/blob/v2.0.6/position.go
- Bubbles v2.2.0 dependency compatibility: https://github.com/charmbracelet/bubbles/blob/v2.2.0/go.mod

### Process and local-socket behavior

- Go `os/exec` command, context, cancellation, and wait behavior: https://pkg.go.dev/os/exec
- Darwin `getsockopt(2)` and local socket options: https://keith.github.io/xcode-man-pages/getsockopt.2.html
- Darwin UNIX-domain sockets: https://keith.github.io/xcode-man-pages/unix.4.html
- Apple XNU socket definitions, including local peer options: https://github.com/apple-oss-distributions/xnu/blob/main/bsd/sys/socket.h
