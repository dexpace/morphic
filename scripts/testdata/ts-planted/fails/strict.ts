// Each marked line holds something tsc must refuse. The first is refused
// whatever the options; the other two only where strict checking is on, so a
// run with it off draws one finding where ts-planted.sh wants three.
export const count: number = "three"; // want "TS2322"

export function length(value: string | null): number {
  return value.length; // want "TS18047"
}

export function identity(value) { // want "TS7006"
  return value;
}
