/**
 * Joins CSS classes, ignoring absent values.
 *
 * Deliberately tiny: yagit needs no class-merging library as long as every
 * component decides its own variants instead of accepting arbitrary classes
 * from the outside.
 */
export function cx(...values: Array<string | false | null | undefined>): string {
  return values.filter((value): value is string => Boolean(value)).join(' ');
}
