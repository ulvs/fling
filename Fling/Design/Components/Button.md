# Button

Three variants for every action in Fling; `primary` is the one thing the view is for.

- **primary** — `fling` fill, `on-fling` text. At most one per view: Send, or Try again on a failed transfer.
- **secondary** — `surface-raised` with a `line-strong` border. Everything else that deserves a button: Add files, Choose device.
- **quiet** — no fill until hover. Cancel, Clear list, inline actions in rows and toasts.
- Sizes: `md` (36px) in sheets and footers, `sm` (28px) inside rows and toasts.
- Labels are verbs, sentence case, no full stop: "Send", "Try again", "Add files".
- Disabled buttons lose their colour entirely; say why nearby ("Pick a device first") rather than relying on the disabled look.

Consumer provides: the label, `onClick`, and `disabled` when the action can't run.
