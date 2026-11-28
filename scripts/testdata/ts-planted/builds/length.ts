// length builds under tsc --strict: each use of the nullable value is narrowed
// first. ts-planted.sh fails if tsc reports anything here.
export function length(value: string | null): number {
  return value === null ? 0 : value.length;
}

export function first<T>(values: readonly T[]): T | undefined {
  return values[0];
}
