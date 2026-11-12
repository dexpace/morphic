# Swift

A Multiplatform SDK reaches iOS through the Objective-C export, which shapes these rules.

- A sealed interface is not exhaustive in a Swift `switch`, so Swift callers need a `default` arm,
  and the generated docs say so.
- A name the export would mangle — `description` and `hash` (`NSObject` members), a name in the
  `init`, `new`, `copy` or `alloc` method families, a Swift keyword, a name another declaration
  exports — gets an explicit Objective-C and Swift name from the [naming
  policy](../../refine/naming.md) (`@ObjCName`), so the Swift name is the policy's choice rather
  than the exporter's. `@ObjCName` is experimental in Kotlin 2.4.0, and a file using it without an
  opt-in fails to compile, so each such file opts in with
  `@file:OptIn(ExperimentalObjCName::class)`. No caller inherits that, and no signature carries it
  ([the language floor](overview.md#the-language-floor)). `@ObjCName` takes ASCII letters, digits
  and `_` alone, a digit never first, in its name and its Swift name alike (Kotlin/Native 2.4.0's
  `FirNativeObjCNameChecker`; `'@ObjCName' contains illegal characters`), so an explicit name for a
  declaration whose name holds a character past ASCII is written in ASCII (below).
- The export renames an enum entry in camel case, splitting it at each `_`, so it loses a break
  letter case cannot mark
  ([naming](../../refine/naming.md#a-break-letter-case-cannot-mark-takes-an-underscore)): measured
  with Kotlin 2.4.0, entries `VALUE_1_0` and `VALUE_10` both became `value10`, the one the class
  declares second taking a trailing `_`, so which value a Swift caller's `value10` named followed
  their order. An entry whose words hold such a break takes an explicit name, its words cased as a
  camel role cases them, and with one `VALUE_1_0` exported as `value1_0` beside `VALUE_10`'s
  `value10`. Entries `A_日本` and `A日本` exported as `a日本` and `a日本_`, the second in
  declaration order taking the `_`, and with `A_日本`'s explicit name, which is in ASCII (below), as
  `aU65e5u672c` and `a日本`.
- A name holding a character past ASCII reaches Swift by its Objective-C name, since clang drops a
  `swift_name` holding one (`'swift_name' attribute has invalid identifier for the base name`).
  Measured with Kotlin 2.4.0 and Swift 6.2.3, a class `A日本` was `ProbeA日本` in Swift, `A日本()`
  failing (`cannot find 'A日本' in scope`); a property `名前` kept its name; a method `取得(値: Int)`
  was `取得値(_:)`, its selector `取得値:`, so `取得(値: 1)` failed; and an ASCII class kept its
  Swift name beside a member `café`. That Objective-C name is the declaration's Swift name, which
  the policy reads as the export forms it, and resolves in the namespace every unit shares (below).
  A declaration past ASCII that takes an explicit name by a rule here takes one in ASCII, its words
  cased as its role cases them and each character past ASCII written as the character-name table
  writes one it does not hold, `u` and its code point in lowercase hex
  ([naming](../../refine/naming.md#a-character-a-target-cannot-write-is-written-by-its-name)). So
  does one whose exported name would hold U+2E2F, which no header can: `@ObjCName("Au2e2f")` on a
  class `Aⸯ` built a module a Swift file called (`Au2e2f()`). Giving every declaration past ASCII
  an explicit ASCII name was not taken: it writes each such name in code points, `u540du524d` for
  `名前`, where the export's names read as the spec's. Nor was writing those characters by name in
  the Kotlin name, which would cost a JVM caller the spec's name for Swift's sake.
- The export drops a declaration's package and joins a nested one's name to its outer's, so every
  unit's declarations, nested ones among them, share one namespace, and of two that share a name it
  appends `_` to one. Measured with Kotlin 2.4.0, within one module and across modules the framework
  exports, the runtime's `ApiException` in `p.runtime` exported as `ApiException_` beside a model
  `p.accounts.ApiException`; a model `PetDog` as `PetDog_` beside the class `Dog` that a sealed
  interface `Pet` nests, which Swift calls `PetDog` too; and a model `UserBuilder` as `UserBuilder_`
  in Objective-C beside `User.Builder`, whose Swift name keeps its nesting. Two declarations collide
  there when their exported names do, whichever units and types hold them, and a name the target
  declares is reserved there in every unit ([naming](naming.md#reserved-names)).
- A Swift caller has Swift's standard library and Foundation in scope beside the framework, and the
  export renames neither side. Measured with Kotlin 2.4.0 and Swift 6.2.3, a framework class named
  `Optional`, `Result` or `Duration` shadowed Swift's own in a caller's file (`cannot specialize
  non-generic type 'Optional'`, `type 'Duration' has no member 'seconds'`), and one named `Decimal`,
  `Date`, `Data` or `URL` made the name ambiguous with Foundation's (`'Decimal' is ambiguous for
  type lookup in this context`), so importing the SDK broke code that never touched it. A
  declaration whose Swift name would be one of theirs takes an explicit one, the runtime's
  `Optional`, `Duration` and `Decimal` among them. A caller settles an ambiguity by naming the
  module (`Probe.Image`, `SwiftUI.Image`), which fails once a type takes the module's name: a class
  `Probe` in the framework `Probe` turned `Probe.Client()` into `type 'Probe' has no member
  'Client'`, and a class `Foundation` turned `Foundation.Date` into `'Date' is not a member type of
  class 'Probe.Foundation'`. So the framework's own name, `Swift` and `Foundation` are reserved too,
  while a module a caller imports by choice is left to qualification: a SwiftUI file's `let i:
  Image` was ambiguous, and `SwiftUI.Image` and `Probe.Image` were not
  ([naming](naming.md#reserved-names)).
- The export names classes of its own after the framework, `ProbeBase` and `ProbeNumber` in
  Objective-C for a framework `Probe`, and boxes each Kotlin number in one (`KotlinInt` in Swift).
  Measured with Kotlin 2.4.0, a spec class `Base`, `MutableSet` or `MutableDictionary` was renamed
  beside them (`ProbeBase_`), but one named `Number` or a boxed number's, `Int`, `Double` or `ULong`
  among them, took the same Objective-C name, and the framework failed to link (`Linking globals
  named 'OBJC_METACLASS_$_ProbeNumber': symbol multiply defined!`). With an explicit `@ObjCName` it
  linked, so each of these names is reserved ([naming](naming.md#reserved-names)).
- The export gives a value class no declaration: a position of its type is `id` in Objective-C and
  `Any` in Swift, and its companion is unreachable. So an open enum, which a Swift caller names
  wherever it sets or reads one, is a final class on Multiplatform ([enums](types.md#enums)).
- Default arguments do not reach Swift, which is one reason models and request types are built
  through a builder rather than a constructor with defaults.
- An exception reaches Swift only as `@Throws` declares it: any other terminates the app, and a
  `suspend` function with no `@Throws` propagates cancellation alone. So every operation carries
  `@Throws(ApiException::class, OtherResponseException::class, CancellationException::class)`, which
  covers every class of the tree ([failures](errors.md)) and a response its method cannot return
  ([return shapes](returns.md)), and a Swift caller receives an `NSError` holding the exception, and
  reads its `failure` or `response`.
