# DropZone

The big drop target that fills the empty window — the first thing people see in Fling.

- Idle: dashed `line-strong` outline on `surface`, an upload mark, the `display` headline "Drop files to fling" and a caption in `ink-muted`.
- File over the window: solid `fling` outline, `fling-soft` fill, the mark lifts 4px and turns `fling`, and the headline changes to "Let go to fling". The change is in outline style and wording, not colour alone.
- It also opens the file chooser on click, Enter or Space, so it works without a mouse.
- Use one per window. Once files are added, shrink the window's drop area to the whole window (drop anywhere) and show TransferRows instead.

Consumer provides: `onFiles(files)`; optional `title`, `overTitle`, `hint`; `disabled` while nothing can receive (e.g. offline — then change the hint to say so).
