// The conditions every yagit capture is taken under.
//
// `./do shot` and `./do drive` both photograph the interface, and the pictures
// are compared across runs and across the two commands — a before from one and
// an after from the other. A viewport changed in one file alone would make
// every such pair meaningless, so the numbers live here and nowhere else. The
// scale factor of 2 is what makes typography judgeable.
//
// The colour scheme is here for the same reason, and it is not geometry. The
// workbench follows the machine's preference unless a reader has chosen
// otherwise, and a headless Chromium's preference is light — so without this
// line the theme in a screenshot would be decided by the browser's default
// rather than by the person taking it, and a before from one machine would not
// compare with an after from another. Dark is pinned because dark is what the
// design system is drawn against; a light capture is `?theme=light` on the
// design page, or a reader's own choice in the workbench.
export const captureViewport = {
  viewport: { width: 1440, height: 900 },
  deviceScaleFactor: 2,
  colorScheme: 'dark',
};

// A page that never answers has to fail with a message rather than hang until
// the caller gives up: on a headless machine there is nobody watching a
// browser that stopped.
export const pageLoadTimeout = 15_000;
