package planted

import . "strings" // want "dot-imports: should not use dot imports (revive)"

// shouted reads the dot-imported ToUpper.
var shouted = ToUpper("x")
