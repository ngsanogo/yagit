# Accessibility

yagit holds itself to [WCAG 2.1](https://www.w3.org/TR/WCAG21/) levels A and
AA. That is not a statement of intent: `./do test e2e` fails the suite if
axe-core finds a violation on any of the screens it scans — the design system
in both themes and with a dialog or a row menu open, the workbench, the changes
view, the conflict view, the interactive rebase plan, and a populated stash
panel.

Colour contrast is the same gate. Every pairing of text and background either
clears 4.5:1 or the end-to-end suite fails. Dark is the default theme; light
is checked as strictly. `prefers-reduced-motion` is honoured in
`web/src/design/base.css`.

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

- Keyboard navigation works end-to-end
- Focus states are visible and logical
- Colour is not the only way to convey meaning
- Reduced motion is respected if you added animation
