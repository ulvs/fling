# Logos

The Fling logo is the **flung dot**: a lowercase "fling" whose i has lost its dot. The dot has been thrown. It travels along a dotted trail and lands to the right of the g, at exactly the height where an i-dot would sit. The logo does what the app does.

## Files

Which one to use: on white or any light ground, **fling-logo.svg**. On dark grounds, **fling-logo-dark.svg**. To make the brand stand out on a white page, **fling-logo-reversed.svg**. Where only one colour is possible, **fling-logo-black.svg** or **fling-logo-white.svg**.

- **fling-logo.svg** — the main logo, for white and light grounds. Letters `ink` #1E1C1A, trail `fling` #BD3514, dot `arc` #F2B73A.
- **fling-logo-dark.svg** — for dark grounds. Letters dark-theme `ink` #F2EEE8, trail dark-theme `fling` #FF7A52, the same `arc` dot.
- **fling-logo-small.svg** — for sizes under about 32px tall (title bars, favicons, footers). The trail is dropped and the dot is a little larger, so it still reads.
- **fling-logo-reversed.svg** — the logo knocked out in white on a solid `fling` block (`radius-md`-style 28-unit corners, built-in clear space), with a `fling-soft` trail and the `arc` dot. For white pages, docs, slides and store listings where the logo should be the loudest thing.
- **fling-logo-black.svg** / **fling-logo-white.svg** — single-colour versions, everything in #1E1C1A or #FFFFFF. For print, stamps, embossing, and photos or brand-coloured grounds where vermilion and yellow would clash. Never set the white one on a light ground.
- **fling-app-icon.svg** — the app icon: a `fling` rounded square with a `fling-soft` throw trail landing on an `arc` disc. It's the same idea as the logo, drawn for the dock and taskbar, and works on light and dark desktops.

## Construction

- One monoline stroke (14 units at 162 tall) with round ends and joins, matching the soft `radius-md` corners. The f's top and the l's ascender align; the f's crossbar sits on the x-height.
- The trail: 12 dots evenly spaced along a parabola from where the i-dot should be, peaking about two x-heights above, and growing from 1.6 to 4 units toward the landing, so it reads as speeding up, not trailing off. A small gap is left before the landed dot.
- The landed dot is the only yellow, and always `arc`, whatever the ground. Only the single-colour versions break this rule.

## Use

- Clear space: at least one x-height (the height of the n) on every side.
- Minimum size: the full logo at 32px tall; below that use fling-logo-small.svg.
- Don't recolour the letters vermilion, move or recolour the dot, add a dot to the i, stretch or skew, or set "Fling" in another typeface in its place.
