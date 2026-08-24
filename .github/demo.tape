# Recording: build lazybrew onto PATH, then: vhs .github/demo.tape
# Re-record whenever the UI changes; this script is the source of truth for the README GIF.
Output .github/demo.gif

Set FontSize 15
Set Width 1200
Set Height 700
Set Padding 12
Set TypingSpeed 60ms

Type "lazybrew"
Enter
# Snapshot-seeded boot paints instantly; give the refresh a beat to land.
Sleep 2.5s

# Browse a few casks; the info pane follows.
Type "jjj"
Sleep 1.5s

# Switch to formulae: versions, sizes, outdated arrows.
Tab
Sleep 3s

# Cycle the sort: size descending — what is eating the disk.
Type "o"
Sleep 2s

# Search.
Type "/"
Sleep 500ms
Type "docker"
Sleep 1.5s
Escape
Sleep 1s

# Strict confirm: open the uninstall dialog, then cancel (only lowercase y proceeds).
Type "d"
Sleep 2s
Type "n"
Sleep 1.5s

Type "q"
Sleep 500ms
