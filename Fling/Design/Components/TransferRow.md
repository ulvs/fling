# TransferRow

One file going to one device, with its live status: waiting, sending, delivered or didn't send.

- **queued** — clock icon, "Waiting", all in `ink-muted`.
- **sending** — `arc` disc with an upload arrow, an `arc` progress bar, and `mono` figures: size · percent · speed · time left. The figures carry the information; the bar is the glance. A quiet Cancel sits on the right.
- **delivered** — `done` check ring and a `done` line "Delivered 14:02 · verified". Only once the receiver has confirmed the file.
- **failed** — warning icon in `ink`, the reason in plain words ("Couldn't reach Anna's MacBook."), and a primary **Try again**. Fling has no error red on purpose: failure is told by the icon, the wording and the action, so it never competes with the brand vermilion.
- File names truncate with an ellipsis; never wrap them.

Consumer provides: `name`, pre-formatted `size`/`speed`/`timeLeft`/`time` strings, `status`, `progress` (0–1), and `onCancel` / `onRetry`.
