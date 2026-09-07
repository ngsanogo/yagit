/** Sun and moon icons for the theme toggle — shared by the workbench and the showcase. */

export function SunGlyph() {
  return (
    <svg width="13" height="13" viewBox="0 0 14 14" fill="none" aria-hidden="true">
      <circle cx="7" cy="7" r="2.75" stroke="currentColor" strokeWidth="1.3" />
      <path
        d="M7 1v1.5M7 11.5V13M1 7h1.5M11.5 7H13M2.8 2.8l1 1M10.2 10.2l1 1M11.2 2.8l-1 1M3.8 10.2l-1 1"
        stroke="currentColor"
        strokeWidth="1.3"
        strokeLinecap="round"
      />
    </svg>
  );
}

export function MoonGlyph() {
  return (
    <svg width="13" height="13" viewBox="0 0 14 14" fill="none" aria-hidden="true">
      <path
        d="M12 8.6A5.4 5.4 0 0 1 5.4 2 5.5 5.5 0 1 0 12 8.6"
        stroke="currentColor"
        strokeWidth="1.3"
        strokeLinejoin="round"
      />
    </svg>
  );
}
