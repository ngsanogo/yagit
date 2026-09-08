# Accessibility

yagit holds itself to [WCAG 2.1](https://www.w3.org/TR/WCAG21/) levels A and
AA. That is not a statement of intent: `./do test e2e` fails the suite if
axe-core finds a violation on any of the screens it scans — the design system
in both themes and with a dialog or a row menu open, the workbench, the changes
view, the conflict view, the interactive rebase plan, and a populated stash
panel.

Colour contrast is measured twice, because the two instruments answer
different questions. `./do test web` reads the declarations in
`web/src/design/tokens.css` and fails below 4.5:1 for every ink on every
surface and every status colour on every base surface — including the pairings
no screen draws yet. `./do test e2e` measures the composite a person actually
sees, on the screens listed above. Dark is the default theme; light is checked
as strictly.

Two limits on that, both deliberate and both written down rather than left to
be rediscovered. One pairing is under the floor today: in the light theme the
status colours — file marks, ref badges, author chips — over a hovered or
selected row, where the best of them reaches 3.76:1. Closing it means
re-deriving every solid status against a highlighted row rather than against
the page, which is a repaint of a whole palette and wants an eye on it; the
argument sits in `tokens.css` beside the values. And axe's third answer is
*incomplete*, not green: a ground it cannot resolve — a translucent
`bg-<token>/<n>`, of which this interface has many — is reported as unmeasured
rather than as a violation, and the scan asserts only on violations.
`web/e2e/accessibility.ts` says what closing that would take.

`prefers-reduced-motion` is honoured in `web/src/design/base.css`.

There is deliberately no `eslint-plugin-jsx-a11y`. No published version
accepts eslint 10, which this project runs, and forcing the peer dependency
leaves a silent hole: the plugin reads JSX, so a missing accessible name that
comes from a runtime prop, a contrast ratio, or a focus order is invisible to
it — and is precisely what axe measures.

yagit is a local web UI served by a daemon. There is no mobile app and no
separate marketing site to keep in sync.

## Reporting

Use the
[accessibility issue template](https://github.com/ngsanogo/yagit/issues/new?template=accessibility.yml).
It asks for the assistive technology, the OS, the browser and a severity,
because a generic bug report does not. The `accessibility` label is applied
for you.

A blocker — cannot complete a core task — is triaged ahead of a contrast
tweak. A workaround, when there is one, will be written on the issue.

## What a pull request that changes the UI must do

The pull request template asks four questions. They are the definition of
done for a UI change, not optional extras:

- Keyboard navigation works end-to-end. A long list is one tab stop and not a
  thousand: the commit list and the change list each hold a single roving tab
  stop and are walked with the arrow keys, plus Home and End — and PageUp and
  PageDown in the commit list, where a page is a viewport less one row so the
  row you left stays on screen. Focus moves; Enter or Space selects, because
  selecting on each press would fetch a commit or a diff per keystroke. A list
  added later is expected to behave the same way.
- Focus states are visible and logical
- Colour is not the only way to convey meaning
- Reduced motion is respected if you added animation
