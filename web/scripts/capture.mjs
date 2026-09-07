// The geometry every yagit capture is taken at.
//
// `./do shot` and `./do drive` both photograph the interface, and the pictures
// are compared across runs and across the two commands — a before from one and
// an after from the other. A viewport changed in one file alone would make
// every such pair meaningless, so the numbers live here and nowhere else. The
// scale factor of 2 is what makes typography judgeable.
export const captureViewport = {
  viewport: { width: 1440, height: 900 },
  deviceScaleFactor: 2,
};

// A page that never answers has to fail with a message rather than hang until
// the caller gives up: on a headless machine there is nobody watching a
// browser that stopped.
export const pageLoadTimeout = 15_000;
