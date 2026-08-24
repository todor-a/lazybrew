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
- Installing, pinning, or otherwise mutating packages except uninstalling or upgrading the explicitly confirmed package. **[REWRITE ADDITION]** Amended, as its previous wording said it would be, by the increment that implemented section 9A: upgrading a confirmed package is now permitted and is the only mutation besides uninstalling. Both go through one confirmation, one snapshot, and one privileged path. Reporting which packages `brew outdated` names remains a read.
- Listing formulae installed only as dependencies.
- **[REWRITE ADDITION]** Installing or pinning dependency-only formulae. Replaces the non-goal `Listing formulae installed only as dependencies.`, which forbade the only view that can answer what is consuming disk: on the measured machine the dependency-only set is 180 of 304 installed formulae and holds 7 of the 12 largest packages, including the single largest at 1.5 GB. Dependency-only formulae are now listed behind an explicit toggle that is off at startup (section 4), so the curated default view is unchanged.

  This expands the destructive surface and the non-goal must not be read as shielding those rows. With the toggle on, a revealed dependency row is an ordinary `d` target like any other: nothing in this application refuses it. The only guards are Homebrew's own refusal to remove a formula another formula needs, and the info pane's `Removing this breaks <n> installed formula(e).` verdict, which is advisory and is withheld entirely when the dependents lookup fails.
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
| `dependency` **[REWRITE ADDITION]** | True when Homebrew reports the formula as not installed on request. Always false for a cask. Display data, not identity, exactly like `version`. |

Package identity and the package-info cache key are `(kind, name)`. Version and `outdated` are display data, not identity.

**[REWRITE ADDITION]** `outdated` replaces nothing; it is a fourth field on a value that previously carried three. It is display data specifically: it stays out of the info cache key, out of the search filter target, and out of every argv, so adding it cannot change which panel is cached, which rows a query matches, or what any command runs. It is set from the section 5 outdated read at list time and is never derived from comparing two version strings.

**[REWRITE ADDITION]** `kind` keeping exactly two values is the reason dependency visibility is a mode on the formula list rather than a third list. A dependency-only formula *is* a formula everywhere it matters — `brew info --formula`, `brew uses --installed`, and `brew uninstall --formula` all behave identically for it, and only the inventory query differs — so a third `kind` would propagate into the list argv, the uninstall argv builder, the kind flag, the list cache, and the `(kind, name)` info key to express a distinction that is one boolean. The two 22-cell tab labels below also leave no room for a third label at the supported 32-column width.

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

**[REWRITE ADDITION]** Because the count follows the live list, it also follows the section 4 dependency toggle with no additional code: on the measured machine the formula list reports `124 formulae installed` with dependencies hidden and `304 formulae installed` with them shown.

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
| `d`, `D` | Open the **uninstall** confirmation for the selected package if one exists and the safety-fit check passes. With no selected package, do nothing. | **[CURRENT/PARITY]** behaviour, **[REWRITE ADDITION]** key |
| `u`, `U` | Open the **upgrade** confirmation, but only for a package `brew outdated` reports. On a package it does not report, start nothing at all — no confirmation, no snapshot, no job — and set the ordinary status `<name> is up to date`. With no selected package, do nothing. | **[REWRITE ADDITION]** |
| `t`, `T` | Cycle to the next theme and set status to `Theme: <name>`. | **[CURRENT/PARITY]** |
| `r`, `R` | Perform the refresh contract in section 8. | **[CURRENT/PARITY]** |
| `a`, `A` | Toggle **all** formulae, dependency-only ones included, into and out of the formula list, resetting selection to the first row, and set status to `Dependencies: shown` / `Dependencies: hidden`. Served from the section 8 list cache: it starts no command and enters no loading state. On the cask list only the status changes and selection is preserved, because casks have no dependency relation — the flag still flips, so the key is never silently dead and a later `tab` lands in the requested state. | **[REWRITE ADDITION]** |
| `o`, `O` | Toggle row order between source order and installed size, largest first, resetting selection to the first row, and set status to `Sort: size` / `Sort: name`. Also served from the list cache with no command. A no-op on order until the section 5 size pass lands, at which point the requested order is applied. | **[REWRITE ADDITION]** |
| `q`, `Q` | Cleanly quit with status 0. | **[CURRENT/PARITY]** |

No Bubbles default key that is absent from this table may remain enabled. In particular, paging, help expansion, fuzzy-search shortcuts, vim left/right paging, and default list quit bindings must not create additional behavior.

**[REWRITE ADDITION]** `d` and `o` were chosen because they are the only unbound single letters left that carry no competing convention here: `S` is already a third search key in the row below, and the list has no delete action for `d` to collide with. Both are normal-mode only. Neither key changes what is installed, so neither invalidates any cache.

### Search-edit mode

Search is case-insensitive substring matching against `name + " " + version + " " + kind`. **[REWRITE ADDITION]** The rendered origin token is appended, so a row displaying `dep` is reachable by that word; `formula` continues to match every formula, including the dependency-only ones whose rows no longer spell it. It preserves source list order; it does not fuzzy-rank results. The Go comparison is the substring relation after applying `strings.ToLower` to both operands.

| Keys | Result |
|---|---|
| Any printable text delivered by a `tea.KeyPressMsg` | Append the text to the query. `q`, `Q`, `u`, `t`, `r`, `s`, `/`, and **[REWRITE ADDITION]** `d`, `D`, `o`, `O` have no global meaning while editing. |
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
| List | formula | `brew`, `list`, `--formula` |
| Dependency set **[REWRITE ADDITION]** | formula | `brew`, `list`, `--formula`, `--no-installed-on-request` |
| Package roots **[REWRITE ADDITION]** | — | `brew`, `--cellar` and `brew`, `--caskroom` |
| Info | cask | `brew`, `info`, `--cask`, `<name>` |
| Info | formula | `brew`, `info`, `--formula`, `<name>` |
| Uninstall | cask | `brew`, `uninstall`, `--cask`, `<confirmed-name>` |
| Uninstall | formula | `brew`, `uninstall`, `--formula`, `<confirmed-name>` |
| Upgrade **[REWRITE ADDITION]** | cask | `brew`, `upgrade`, `--cask`, `<confirmed-name>` |
| Upgrade **[REWRITE ADDITION]** | formula | `brew`, `upgrade`, `--formula`, `<confirmed-name>` |
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
| Installed size **[REWRITE ADDITION]** | formula | `/usr/bin/du`, `-k`, `-d`, `1`, `<cellar>` |

**[REWRITE ADDITION]** The Caskroom is not measured, and no size is offered for a cask - not in the row, not in the header total. A cask's Caskroom entry is frequently a symlink to a bundle in `/Applications`, which `du` reports as about 12 KB; where it is not a symlink it may be a leftover installer package, which reports the installer rather than the application. Measured on the development machine: 29 of 39 casks read under 1 MB, `alt-tab` read 12 KB against a real 12 MB bundle, `google-chrome` read 683 MB only because the Caskroom holds a duplicate bundle, and the largest cask row was a 1.1 GB leftover Excel installer against a 2.4 GB application. Those numbers are not sizes.

The consequence is deliberate and is the same restraint applied elsewhere in this document: where a number cannot be established, none is shown. The size column is reserved on the formula list only, and the header total is the Cellar's and renders on the formula list only, because a Cellar figure above cask rows would name a fleet the screen is not showing. The Cellar is 9.2 GB of the 11.3 GB this feature exists to account for, so the question is still answered where it can be answered honestly.

**[REWRITE ADDITION]** The formula list vector replaces `brew`, `list`, `--formula`, `--installed-on-request`. That filter did not merely omit dependencies, it silently omitted formulae the user had explicitly requested. Measured: `--installed-on-request` reports 116 and `--no-installed-on-request` reports 180, against 304 installed — eight formulae appear under neither filter (`acli`, `codeburn`, `oh-my-posh`, `opencode`, `sshfs-mac`, `terraform`, `tidy-json`, `vault`), all third-party-tap installs whose `INSTALL_RECEIPT.json` records `"installed_on_request": true`, one of them 507 MB. Enumerating the unfiltered list and using `--no-installed-on-request` only as a marker surfaces those eight with the toggle off, at no extra cost, and keeps the row set reconcilable with the measured total. The eight-formula figure is machine- and version-specific and is recorded as evidence, not pinned as an invariant.

**[REWRITE ADDITION]** The enumeration and the marker read run concurrently. They are independent reads of the same inventory, so a formula load costs the slower of the two rather than their sum. Measured warm on the development machine: `brew list --formula` about 0.04s and `brew list --formula --no-installed-on-request` about 0.46s, so about 0.46s rather than 0.50s; measured cold earlier the same reads were about 0.64s each, so about 0.64s rather than 1.27s. The saving is bounded by the slower read and grows with it, which matters because this is the load the list cache exists to avoid repeating.

Their invocation order is therefore not a guarantee, and no test may assert one. The marker read stays load-bearing: its failure fails the whole load rather than degrading to an unmarked list, because an unmarked list would show every dependency as installed on request and the toggle that reveals them would silently show nothing. Where both fail, the enumeration's error is reported, being the more useful of the two.

The marker call is load-bearing for every formula list load and its failure fails the whole load rather than degrading. Rendering 304 rows all labelled on-request would misreport removal safety on a screen that feeds a destructive action. Use `--no-installed-on-request`, not `--installed-as-dependency`: Homebrew prints `Warning: Calling the --installed-as-dependency switch is deprecated! Use --no-installed-on-request instead.`

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
- Bubbles `help` to render the mode-specific footer bindings, with its short separator and short key/description styles supplied rather than blanked.
- Bubbles `viewport` with `SoftWrap = true` for package info.
- Bubbles `spinner` while uninstall or a package-list reload is active.
- Bubbles `textinput` with `EchoPassword` for password entry. The search filter may use the list's filter input, but must satisfy the exact search contract above.
- Lip Gloss joins for the base layout and Lip Gloss v2 `Layer`/`Compositor` for centered overlays.

Do not use Huh or another modal package. `View` is pure: it renders state and must never cancel an operation, clear a modal, mutate selection, or perform I/O.

### Base layout

**[CURRENT/PARITY]** At 32×9 or larger, the screen contains:

1. A full outer border.
2. Header row: the section 3 list tab bar, alone. **[REWRITE ADDITION]** Parity rendered `lazybrew [<theme>]  <active-list>  Tab: switch`; all three parts around the tab bar are removed. The app name is redundant on a screen the user just launched. The theme name is permanent chrome for a value that only matters while cycling it, and `t` already reports `Theme: <name>` in the status row on demand. The `Tab: switch` hint named no destination; the tab bar itself does, and `tab switch` is now carried in the normal footer. **[REWRITE ADDITION]** The header additionally carries the section 5 fleet total, right aligned against the interior edge, once the measurement has landed. It is held to the same standard that removed the app name and the theme name: unlike those, the total is the question this screen exists to answer and is always relevant, and the status row is already contested by query, count, and errors and is the first thing clipped at 32 columns. The value is bare, with no label, because 22 cells of tab bar plus one gap plus at most 6 cells of value fits the 30-cell minimum interior and a labelled form does not. It reports the whole fleet, not the visible subset, so it does not flicker as the query or the dependency toggle changes. It is omitted rather than clipped whenever the interior cannot hold `label + 1 + value`, so the pinned tab-bar cell slots are never disturbed.
3. A horizontal rule.
4. A package content region of exactly `height - 7` rows.
5. A horizontal rule.
6. One status row.
7. One footer/help row.

The normal footer's exact logical string is `Search: / | Switch: tab | Uninstall: d | Upgrade: u | All: a | Sort: o | Theme: t | Refresh: r | Quit: q`. Render that string through the mode keymap; Bubbles help must not substitute shorter or alternate labels.

**[REWRITE ADDITION]** The action leads and the key answers it, joined by ` | `, replacing parity's key-first `<key> <label>` pairs separated by two spaces. A footer is read by someone looking for a verb, not by someone scanning single letters. `Search:` advertises `/` alone; `s` and `S` still work and are still bound, they are simply not spelled out.

The label occupies each binding's help-key slot and the keystroke its description slot, which is the reverse of those field names. Bubbles renders key-then-description with no option to swap them, and these bindings are already display-only: nothing dispatches through them, and the progress and cancel entries carry no real key at all. The footer styles compensate, so the keystroke remains the emphasised half.

The footer is two-tone: the action in the footer role, the keystroke bold, the separator faint. Every segment carries the complete role rather than relying on an enclosing style, and the trailing pad is rendered through that role too. A single enclosing style over pre-styled segments ends its own background at the first inner reset, which is visible as a bare strip on any theme whose footer has a background.

Every footer is ANSI-aware clipped, never reworded, to the interior width. At the supported 32-column outer width, the interior is 30 cells and the normal footer content is the first 30 cells, exactly `Search: / | Switch: tab | Unin`: the clip falls inside `Uninstall:`. **[REWRITE ADDITION]** Rebinding uninstall to `d` and adding `Upgrade: u` and `All: a` left this prefix untouched, because `Uninstall:` stayed third; that is the constraint the ordering exists to satisfy. Structural view tests must compare the ANSI-stripped cell sequence and width, not merely search for help labels.

Package rows render a selection marker, a freshness cell, name, kind, and optional version. **[REWRITE ADDITION]** The row shape is ` <marker><freshness> <name-column> <kind>[ <version>]`, where the marker is `>` only for the selected row and `<freshness>` is one fixed cell holding `↑` for a package `brew outdated` reports and a space otherwise. This replaces the earlier ` <marker> <name-column> <kind>[ <version>]`, which had no cell for the outdated verdict. The name column is at least 8 and at most 30 cells when space permits. ANSI-aware clipping must keep every row inside its assigned pane and must never overwrite a divider or border.

**[REWRITE ADDITION]** The freshness cell sits immediately after the selection marker, so the narrow layout — where the list is the only pane and the marker matters most — cannot clip it away. It is taken from the name column, and only where the 30-cell cap is not already absorbing the slack: the name column arithmetic becomes `min(30, max(8, pane-width - 5 - len(kind)))`, one cell tighter than the previous `pane-width - 4 - len(kind)`. At 32 columns a cask row's name column goes from 22 to 21 cells and a formula row's from 19 to 18; at 72 columns from 27 to 26; at 80 columns and wider it stays 30, because the cap already discarded that cell. No pane, divider, border, or total row width changes.

Those counts are against the full row pane. A visible scrollbar takes one further cell from the same arithmetic, so with one present each count above drops by one — at 32 columns an unfiltered formula row's name column is 17, not 18. The pinned numbers therefore describe a list short enough or filtered enough to need no scrollbar; both values follow from the same expression, whose input is the row pane width rather than the list pane width.

The cell carries a glyph rather than a color, for the same reason the scrollbar thumb does: the cue must survive a monochrome theme. Because it lives inside the row string it inherits the selected-row style, including the monochrome reverse+bold, with no nested style. `↑` is an East-Asian-ambiguous-width character; the pinned Lip Gloss v2.0.6 measures it as one cell, and `•` and `█` are existing precedent for such a glyph here. A terminal configured to render ambiguous glyphs double-wide would shift every row by one cell at runtime — a terminal setting no test in this repository can observe. A unit assertion on the measured width therefore guards a dependency bump, not a terminal configuration.

The marker is not appended after the version: at 32 columns the pane is 30 cells and the row is already full at the kind column, so a trailing marker would be clipped exactly where the list is the only pane. Nor is it folded into the kind column as `cask*`: that would make the name column width differ per row, and the kind column would stop aligning within a pane — the same alignment reason the info panel's label width is fixed. Nor is it folded into the selection-marker cell, which cannot carry two independent bits without a cryptic third glyph.
Package rows render a selection marker, name, origin, optional version, and size. **[REWRITE ADDITION]** The row shape is ` <marker> <name-column> <origin-column>[ <version>] <size-column>`, replacing ` <marker> <name-column> <kind>[ <version>]`. The marker is `>` only for the selected row. The name column is at least 8 and at most 30 cells when space permits. ANSI-aware clipping must keep every row inside its assigned pane and must never overwrite a divider or border.

The `<origin-column>` is the parity kind column, repurposed. It is fixed-width per list rather than per row — `cask` at 4 cells on the cask list; `formula` or `dep` padded to 7 on the formula list — so the name column cannot shift between rows. In a single-kind list that column was a constant, so carrying `dep` there spends no width and turns a constant into the dependency marker; with the toggle off no row ever reads `dep`, so the default formula view is unchanged. `dep` is text, not color, per the monochrome precedent the tab bar sets. The column was repurposed rather than removed because it is parity behaviour and re-deriving every width would be the larger change.

The `<size-column>` is 7 cells: one space plus 6 right-aligned. It is ALWAYS reserved once `rowWidth - 4 - originWidth - 7 >= 8`, and rendered blank until the section 5 pass lands, so the name column never reflows when sizes arrive seconds after first paint. `nameWidth = min(30, max(8, rowWidth - 4 - originWidth - sizeWidth))`. The worst case is the 32-column narrow layout with a scrollbar present: `rowWidth` 29, origin 7, size 7, giving a name column of 11 — so the pinned bound of at least 8 holds at every supported size.

Sizes are spelled the way Homebrew's own info output spells them: `<n>KB` below 1024 KB, `<n>MB` below 1024 MB, `<n.n>GB` below 100 GB, and `<n>GB` above it. The value is at most 6 cells below 10000 GB, which is what the reserved column is sized for; a package with no measurement renders an empty column rather than a zero.

**[REWRITE ADDITION]** The row column and the info pane do not agree, and must not be described as agreeing. The row is `du` allocated blocks rendered 1024-based; the info pane is Homebrew's own decimal byte sum. Measured: `llvm@22` renders 1.5GB in the row against 1.6GB in the pane, `qemu` 696MB against 729.1MB, and `go` 270MB against 239.8MB - 13% apart, and in the opposite direction from llvm. They answer the same question by different accounting, and a reader must not treat them as interchangeable.

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

### Installed size measurement [REWRITE ADDITION]

Every row carries a size, and the header carries a fleet total, from ONE `du` pass:

1. `brew --cellar` and `brew --caskroom` (measured 55 ms and 45 ms) print the two roots. The paths are never hardcoded; these documented commands are the portable argv seam.
2. `/usr/bin/du`, `-k`, `-d`, `1`, `<cellar>`, `<caskroom>` — one process, not one per package. Measured 2.1 s warm and 5.6 s cold over 11.3 GB, emitting 345 rows: 304 Cellar children, 39 Caskroom children, and one total per root.

There is no argv-only alternative. `brew info --json=v2 --installed` is a single call carrying description, license, homepage, and install time, but it has no installed size — the same reason recorded at the end of the previous subsection. Per-package `brew info` would be 304 calls at ~400 ms. The filesystem read is not a shortcut around an API; there is no API.

**Parse contract.** Rows are `<kb>\t<path>`. A direct child of a root is one package keyed by `filepath.Base`, bucketed by `filepath.Dir` against the two roots; a row whose path *is* a root carries that root's total, and the fleet total is the sum of the root rows actually seen. Malformed rows are skipped. `du` exits nonzero for a single unreadable subdirectory while still measuring everything else, so a nonzero exit keeps whatever parsed; only a pass that parsed nothing is reported as a failure.

**Layout bound.** The assumption is one sentence and one directory level: each direct child of those two roots is one package, named by the package name. Verified as a zero-diff match in both directions against `brew list --formula` (304 Cellar children) and `brew list --cask -1` (39 Caskroom children).

**Degradation.** A moved or renamed root makes `du` fail or match nothing; rows then render a blank size column and the header shows no total. Nothing claims a wrong number and nothing claims safety, matching the restraint the info pane already applies above.

**What the number means.** It is Homebrew's own on-disk footprint under those two roots, which is what the fleet total is the sum of. For a cask whose Caskroom entry is a symlink into `/Applications` the row therefore reads the size of Homebrew's bookkeeping, not of the application bundle — measured example: `alt-tab` reads 12KB in the row while its info pane reports 12.2 MB, whereas `google-chrome` stores a real 683 MB bundle in the Caskroom and reads it. Symlinks are deliberately not followed: following them would count bytes Homebrew does not own, double-count a bundle reachable from two places, and break the property that the rows reconcile with the total.

**Security invariant.** Names read from `du` stdout are only ever used as map keys, looked up against names that came from `brew list`. They never reach any argv. The size path therefore cannot introduce an unsafe package name, and `safePackageName`, `PrepareUninstall`, and `MapCommandFailure` are untouched. `du` shares the one capture/`WaitDelay`/failure-mapping body with brew, so no second command runner or failure mapper exists; the consequence is that the no-output exit fallback words itself as brew, which is preferred over duplicating the mapper for one message.

`/usr/bin/du` is an absolute path, not a PATH lookup: macOS-only is already a non-goal above, `/usr/bin/du` is SIP-protected base system, and resolving it through the child environment would only add a hijack surface for a process this application spawns.

The measurement is not persisted across runs and not revalidated. `r` is the documented remedy for stale inventory and applies equally here.

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

The ordinary confirmation dialog contains exactly:

- Title: `Confirm uninstall`
- Body: `Uninstall <snapshot-name>?`
- Help: `y: confirm  other: cancel`

For uninstalling the `lazybrew` cask itself, replace the body with `This removes lazybrew itself; the app will exit when done`.

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

**[REWRITE ADDITION]** The section 5 size pass runs in its own `tea.Cmd`, started from `Init` beside the first list command and restarted only by the two cache-invalidation sites. Its contract:

- It never runs in `Update`, never enters a loading mode, never activates the spinner, and never blocks or gates a list result. The list renders and is fully navigable before the measurement lands.
- Its result is latest-only against a generation counter, exactly like list and info; a superseded result is discarded.
- On failure it writes an ordinary, non-priority status and only into an empty status slot, so it can never displace `Uninstalled <name>` or any other real message.
- It registers its context and completion handle with the supervisor, so quitting cancels it and awaits its typed result message before exit, per section 13 and acceptance criterion 18.
- When the measurement lands while a size sort is active, the retained list is re-ordered. Selection resets to the first row, because the order changed underneath the cursor; this can happen at most once, and only if `o` was pressed before the pass landed.

### List cache

**[REWRITE ADDITION]** Every successful list result is retained per kind. A kind switch to a retained kind is served from that retention: it starts no command, enters no loading state, and renders the target list in the same frame as the key press. Only a kind not retained this session starts a `loadSwitch` command. Parity re-ran `brew list` on every switch, so each one cost a fresh empty pane.

Map presence is the retention test, not list length. A kind with nothing installed is retained as an empty list and is a hit, rather than re-shelling on every switch.

Retention is dropped wholesale — both kinds — at exactly the two sites that already drop the info cache, and only there. **[REWRITE ADDITION]** The second site is reached by either privileged verb: the committed-operation reload is one shared code path, so implementing upgrade added no third site. A third call site would mean a forked state machine, which is what generalising the seam exists to prevent.

1. `r` or `R` refresh, per the section 8 refresh contract. Refresh means the inventory may have changed outside the app, so no kind stays trusted.
2. A committed uninstall **or upgrade**, before its `loadAfterOperation` reload starts. Mutating one kind can change the other, so both are dropped rather than reasoning about which.

The info cache and the list cache are invalidated together at both sites and must stay that way: anything that can change what `brew info` prints can change what `brew list` prints. After either site the reload repopulates only the active kind, so the next switch is a miss and re-lists.

**[REWRITE ADDITION]** The section 5 size map is a third cache dropped at exactly those same two sites and only there, for the same reason: anything that can change what `brew list` prints can change what the Cellar weighs. The invalidation function returns the command that restarts the size pass, so both sites restart it in lockstep by construction rather than by each remembering to. A kind switch and the `a`/`o` toggles are explicitly not invalidation sites — they change the view, not the inventory — and must not remeasure. A failed size pass caches nothing, mirroring the rule that a failed list result caches nothing; it also does not discard a previous good measurement.

Retention is per session and is not revalidated. An install or uninstall performed in another terminal is not observed until `r`. Parity's per-switch re-list masked that, at the cost of the empty pane on every switch; `r` is the explicit remedy.

A failed list result caches nothing, so a broken Homebrew never poisons retention. The other kind's earlier retention does survive that failure and can still be switched to.

**[REWRITE ADDITION]** The retained per-kind list carries its outdated marks. A cached kind switch therefore renders them in the same frame as the key press and still starts no command; the marks are part of the retained value, never re-fetched.

A failed outdated read is absorbed, exactly as a failed dependent lookup is. The list result is still a success, is still retained, and simply carries no marks; the detail panel keeps its unmarked wording. A broken outdated read must never poison retention, fail the load, or render as an assurance that nothing is outdated.

The marks are per session and are not revalidated, on the same terms as the list itself: an upgrade performed in another terminal is not observed until `r`, which is the explicit remedy. Because the marks are baked into the formatted text the info cache holds, the info cache and the list cache must continue to be dropped together at every invalidation site; a site that dropped one but not the other would let a stale marker disagree with a stale panel.

The key, priority-status, and footer contracts while a list command is active are exact:

| Load state | Priority status with active spinner | Footer | Ordinary keys |
|---|---|---|---|
| Startup cask load | `Loading casks...` | `Quit: q` | `q` or `Q` enters supervised quitting; every other ordinary key is ignored. |
| Kind-switch load | `Loading casks...` or `Loading formulae...` | `Quit: q` | `q` or `Q` enters supervised quitting; every other ordinary key is ignored. |
| Cached kind switch | none; no command runs | `Search: / | Switch: tab | Uninstall: d | Upgrade: u | All: a | Sort: o | Theme: t | Refresh: r | Quit: q` | **[REWRITE ADDITION]** No load state is entered, so normal mode keeps every ordinary key. |
| User refresh | `Refreshing casks...` or `Refreshing formulae...` | `Quit: q` | `q` or `Q` enters supervised quitting; every other ordinary key is ignored. |
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
3. Return a non-secret `tea.Cmd` that calls synchronous `Runner.Start(context, operation, snapshot)`. `Start` must never execute inside `Update`.
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
- On successful uninstall of the `lazybrew` cask itself, cleanly quit instead of reloading the inventory.

**[CURRENT/PARITY]** Except for the self-uninstall exit above, success is observable only after both `brew uninstall` and the active list reload succeed.

## 9A. Upgrade

**[REWRITE ADDITION]** Implemented. This section was a design; the increment that shipped it amended section 2's mutation non-goal, section 4's key table, section 5's argv table, section 6's footer, and section 12's file list and singular-seam rule, all in the same change.

`brew upgrade` on a cask can require administrator authentication exactly as `brew uninstall` can, so the upgrade action reuses the whole `internal/privileged` machinery — private askpass endpoint, kernel peer authentication, tracked process group, bounded cleanup, immutable confirmation snapshot. That machinery was generalised, never forked. A second copy of the security path would have been a worse outcome than no upgrade action at all.

### One seam, two verbs

`brew.PrepareUninstall(env, pkg)` became `brew.PrepareCommand(env, op, pkg)`:

```go
type Operation uint8

const (
    Uninstall Operation = iota
    Upgrade
)

func (o Operation) Verb() string
func PrepareCommand(env []string, op Operation, pkg Package) (ResolvedCommand, error)
```

`Verb()` returns the brew subcommand, and is also the word every user-facing string is built from, so the argv and the wording cannot disagree about which operation is running. An operation outside the two constants is rejected before anything else, and the unsafe-name refusal is per operation — `Unsafe package name; uninstall refused` or `Unsafe package name; upgrade refused` — still starting no process.

The section 12 rule is restated **stronger**, not weakened: no second package validator, command argv builder, executable resolver, or command-failure mapper may exist, for either verb. Dropping the word "uninstall" widened the rule's scope while keeping it singular.

### `internal/uninstall` → `internal/privileged`

A package named for one verb cannot honestly own two. `Uninstaller` became `Runner`, `Start(ctx, pkg)` became `Start(ctx, brew.Operation, brew.Package)`, and three error strings lost the verb: `Could not start %s: %w` now takes it, while `fatal cleanup failure` and `workers did not stop before cleanup deadline` no longer name one. `Job`, `Event`, `Result`, `RequestID`, the helper dispatch, and every invariant in sections 10, 11, and 13 are untouched.

The rename touched every file of a security-critical package, where silently dropping a test file or one of its package-level `var` seams is the highest-consequence mistake available in this repository: a seam that stops being overridden still compiles, still vets, and still passes. The increment therefore verified it file by file. Recorded so a future rename repeats the check: **19 seams**, with override counts unchanged before and after, and **37 tests** across three test files, unchanged. `prepareUninstall` → `prepareCommand` was the only seam renamed.

The same argument renamed the identifiers in `internal/ui` that had come to cover both verbs: the model's runner field, `finishOperation`, `loadAfterOperation`, and `modeOperation`.

### Per-operation strings

The verb is captured with the confirmation snapshot and is as immutable as the snapshot, so a later selection change cannot retarget an in-flight operation's wording. The spellings are written out as data rather than derived from the verb, because "upgrade" gerunds to "upgrading" and "uninstall" to "uninstalling" — any suffix rule would be wrong for one of them.

| Uninstall | Upgrade |
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

`Cancelling <name>...`, `Widen terminal to confirm`, `Administrator authentication failed`, and `Administrator authentication timed out` are shared and unchanged. The lowercase-`y`-only confirmation discipline is reused verbatim and is never re-derived.

The fit check measures the longest string of **both** verb sets and both confirmation modals, so a terminal wide enough to confirm one verb can never be too narrow to render the other mid-operation.

### Keys, and two deviations from this design

Uninstall moved from `u` to `d`; `u` starts an upgrade; the dependency toggle moved from `d` to `a`. That is the first deviation: this section originally specified `g` for upgrade. The footer became `Search: / | Switch: tab | Uninstall: d | Upgrade: u | All: a | Sort: o | Theme: t | Refresh: r | Quit: q`, and the section 6 exact 30-cell prefix at width 32 is unchanged, because `Uninstall:` stayed third.

`u` on a package that `brew outdated` does not report starts nothing at all: no confirmation, no snapshot, no job. It sets the ordinary status `<name> is up to date`. The privileged machinery stays unreachable for an operation that would be a no-op, and the section 6 freshness cell is exactly the affordance that tells the user which rows `u` acts on.

`u` now performs a different mutation than it did before this change, where it uninstalled. No migration notice is shown. The guard is the confirmation itself, which names both the verb and the package and accepts lowercase `y` only, and whose wording differs between the two verbs in the title and the prompt.

### The cache-invalidation sites

This section's design predicted a third invalidation site. There are still **two**, and that is the second deviation: the committed-operation reload is one code path shared by both verbs, so a third call site would mean a forked state machine — precisely what generalising the seam exists to avoid. The sentence in section 8 is therefore amended to say two sites, the second of which is reached by either privileged verb, rather than three.

The pairing rule is unchanged and is what makes the shared site safe: either verb changes both what `brew list` prints and what `brew info` prints, and the section 6 outdated marks are baked into the cached panel text, so dropping one cache without the other would leave a stale `↑` beside a fresh panel.

### What was not done

The capability-gated real-`sudo` positive test still exercises `brew uninstall` against a fixture tree. Pointing it at `brew upgrade` would mutate a package the test cannot restore, so upgrade's privileged path is covered by the same fake-job and seam-override tests as uninstall, plus the argv pinning above, and not by a real privileged upgrade. Driven manually to the confirmation and cancelled there, for the same reason.

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
  internal/privileged/privileged.go
  internal/privileged/protocol.go
  internal/privileged/peer_darwin.go
  internal/brew/*_test.go
  internal/info/*_test.go
  internal/ui/*_test.go
  internal/privileged/*_test.go
```

No non-Darwin peer adapter or generic package-manager layer is planned.

### Bubble Tea UI module: `internal/ui`

Interface:

```go
func New(homebrew Homebrew, info *info.Loader, runner Runner) (tea.Model, *Supervisor)
func (s *Supervisor) Cleanup(context.Context) error
```

**[REWRITE ADDITION]** `Sizes` is placed on the `Homebrew` interface rather than passed as a fourth parameter to `ui.New`, because that signature is pinned directly above and because the measurement genuinely is a Homebrew read: it needs `brew --cellar` and `brew --caskroom`.

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
    Sizes(context.Context) (Sizes, error)            // [REWRITE ADDITION]
}

// [REWRITE ADDITION] Installed size in KB, keyed by kind and name.
type Sizes struct {
    Formula map[string]int64
    Cask    map[string]int64
    Total   int64
}

func (s Sizes) KB(kind Kind, name string) (int64, bool)

type ResolvedCommand struct {
    Path string
    Args []string
}

func PrepareUninstall(env []string, pkg Package) (ResolvedCommand, error)
func MapCommandFailure(runErr error, stdout, stderr []byte) error
```

`PrepareUninstall` is the narrow uninstall boundary: it applies the same package-name validator used by `Info`, resolves `brew` through `PATH` in the supplied canonical child environment, and returns only the resolved executable plus arguments exactly equal to the uninstall table after `brew`. `MapCommandFailure` implements the single lookup/start/nonzero-exit mapping in section 5; it returns nil for a nil run error. The real adapter and uninstall module both use these functions. No second package validator, uninstall argv builder, executable resolver, or command-failure mapper may exist.

**[REWRITE ADDITION]** `Outdated` is a plain read on the same seam: it reuses the one kind-flag table, the one `run` helper, the one name parser, and `MapCommandFailure`, and adds no validator because it passes no package value. A missing `brew` reported through `Outdated` must produce the same exact text as through `List`, `Info`, or `PrepareUninstall`.

The real adapter hides process execution/reaping, stdout/stderr capture, list parsing, and UI-facing calls. `internal/privileged` alone adds the canonical askpass environment, process-group ownership, broker, and cleanup to the prepared command; the brew seam does not expose arbitrary commands. A fake adapter supplies deterministic lists/info/errors in model and loader tests.

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

### Privileged/askpass module: `internal/privileged`

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
2. **[REWRITE ADDITION]** Formula listing enumerates every installed formula through `brew list --formula` and marks dependency-only rows from `brew list --formula --no-installed-on-request`; the default view hides the marked rows and `d` reveals them. This replaces "excludes dependency-only formulae through `--installed-on-request`", which also hid explicitly requested tap formulae.
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
21. **[REWRITE ADDITION]** Dependency-only rows are hidden at startup and revealed only by `d`, marked in the origin column, served from retention with no command in either direction, and the installed count follows the toggle.
22. **[REWRITE ADDITION]** Every row carries a size once one cached `du` pass has landed; the list renders and is fully navigable before it lands; the size column is reserved so no column reflows when it does; and an unmeasured package renders blank rather than zero.
23. **[REWRITE ADDITION]** `o` orders rows by size largest-first and back to source order, the query filter preserves that order, and the fleet total is visible in the header.
24. **[REWRITE ADDITION]** The size map is invalidated at exactly the two existing cache-invalidation sites and nowhere else, the retained list is never mutated by the filter or the sort, and the size child is cancelled and reaped on every exit path.

## 17. Verification matrix

| Area | Required verification | Observable pass condition |
|---|---|---|
| Command vectors | Table-driven unit tests for both kinds and list/info/uninstall/outdated, including unsafe names. **[REWRITE ADDITION]** The outdated cases assert both vectors and that `--greedy` never appears, and that an invalid kind starts no process. | Exact argv equality; shell path is never used; unsafe info/uninstall text is exact and no process starts; the outdated vectors are exactly `outdated --cask --quiet` and `outdated --formula --quiet`. |
| Parsing/errors | Unit tests for blank lines, empty/multiple versions, missing brew, generic spawn error, stderr/stdout/fallback exit errors, and shared preparation/mapping calls. | Results and strings exactly match sections 3 and 5; uninstall has no duplicate validator, argv builder, resolver, or failure mapper. |
| Keys/modes | Bubble Tea model tests send every listed key/message plus representative unlisted keys in every mode, including `KeyBackspace`, separately delivered `ctrl+h`, physical `KeyDelete`, and `tea.PasteMsg`. | Exact transition tables; `y` confirms, `Y` cancels, search/password steal global letters, loading modes supervise `q`, progress/reload ignore controls, physical Delete and password paste are ignored. |
| Filtering/selection | Model tests edit/accept/cancel queries, switch kinds, reload shorter/empty lists, and resize viewports. | Source-order substring results and all indices/offsets remain valid. |
| Layout | Golden or structural view tests at 31×8, 32×9, 71×N, 72×N, narrow names, long names, long status, and multiline info. **[REWRITE ADDITION]** Plus: an outdated row renders ` >↑ ` and a fresh row ` >  ` at identical total width; the freshness cell is independent of the selection marker; and the measured width of `↑` is asserted to be one cell. | Minimum-size message, split threshold, clipping, wrapping, status, and no border/divider overwrite; at width 32 the ANSI-stripped normal footer is exactly the 30-cell prefix specified in section 6; every row is exactly the terminal width at 32, 72, and 120 columns with marked and unmarked rows both on screen, and the section 6 name-column counts hold. |
| Command vectors | Table-driven unit tests for both kinds and list/info/uninstall, including unsafe names. **[REWRITE ADDITION]** Also both formula list vectors as an ordered invocation sequence, the two root vectors, and the `du` vector. | Exact argv equality, including the complete ordered sequence of invocations per operation; shell path is never used; unsafe info/uninstall text is exact and no process starts. |
| Installed size **[REWRITE ADDITION]** | Unit tests over real captured `du` output for exact values and both root totals; malformed, out-of-level, and foreign rows; nonzero exit with partial output; a missing root; both roots missing; an empty root path; missing brew. An integration test runs the real `/usr/bin/du` over a Homebrew-shaped temporary tree. | Exact per-package KB and fleet total; unreadable rows skipped rather than guessed; partial measurement kept and a wholly failed pass reported; `-k` and `-d 1` verified by observed behaviour rather than by a recorded string; names from `du` never reach an argv. |
| Dependency visibility **[REWRITE ADDITION]** | Model tests toggle `d` on both lists, assert the visible set, the status, the installed count, that no command starts, that retention is not mutated, and that a round trip is idempotent. | Marked rows hidden by default and revealed by `d`; cask list changes status only and preserves selection; `listCalls` unchanged; retained slice byte-identical after any sequence of toggles. |
| Parsing/errors | Unit tests for blank lines, empty/multiple versions, missing brew, generic spawn error, stderr/stdout/fallback exit errors, and shared preparation/mapping calls. | Results and strings exactly match sections 3 and 5; uninstall has no duplicate validator, argv builder, resolver, or failure mapper. |
| Keys/modes | Bubble Tea model tests send every listed key/message plus representative unlisted keys in every mode, including `KeyBackspace`, separately delivered `ctrl+h`, physical `KeyDelete`, and `tea.PasteMsg`. | Exact transition tables; `y` confirms, `Y` cancels, search/password steal global letters, loading modes supervise `q`, progress/reload ignore controls, physical Delete and password paste are ignored. |
| Filtering/selection | Model tests edit/accept/cancel queries, switch kinds, reload shorter/empty lists, and resize viewports. | Source-order substring results and all indices/offsets remain valid. |
| Layout | Golden or structural view tests at 31×8, 32×9, 71×N, 72×N, narrow names, long names, long status, and multiline info. **[REWRITE ADDITION]** Also 120×N, the origin and size columns, and the header total, each asserted with the measurement both landed and absent. | Minimum-size message, split threshold, clipping, wrapping, status, and no border/divider overwrite; at width 32 the ANSI-stripped normal footer is exactly the 30-cell prefix specified in section 6, unchanged by the two added bindings. **[REWRITE ADDITION]** Every row is exactly the terminal width in both size states; the origin column sits at identical cells in both, so nothing reflows when sizes land; the total is right aligned, present at the 32-column minimum, absent before measuring, and omitted rather than clipped when it cannot fit. |
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
