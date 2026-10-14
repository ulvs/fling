# Toast

A short floating message after something finishes — the only component that uses `shadow-float`.

- **done** — `done` check: "3 files delivered to Anna's MacBook".
- **neutral** — file icon in `ink-muted`: "Link copied".
- **alert** — warning icon in `ink`, with an action: "Couldn't reach Anna's MacBook" + Try again.
- One sentence, no full stop needed, no exclamation marks. At most one action, as a quiet button.
- Show one toast at a time, bottom-centre of the window, for about 4 seconds (alerts stay until dismissed).

Consumer provides: the message as children, `tone`, optional `actionLabel` + `onAction`, and `onDismiss`. Positioning and timing are the app's.
