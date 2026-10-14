# DevicePicker

A single-choice list of nearby devices to send to, built as a radio group.

- Each device: a round avatar with a laptop, desktop or phone icon, the device's own name, and a second line ("Same Wi-Fi", "Nearby").
- Selected: `fling-soft` row, `fling` border, filled `fling` avatar and a check — three cues, not just colour.
- Unreachable devices stay in the list, greyed, with "Not reachable", so people understand why theirs isn't selectable.
- Show device names exactly as their owners named them.

Consumer provides: `devices`, the selected `value`, `onChange(id)`; optionally `label` (default "Send to").
