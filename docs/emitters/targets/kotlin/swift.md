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
  ([the language floor](overview.md#the-language-floor)).
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
- The export's own classes take `Kotlin` before their names in Swift (`KotlinBase`, `KotlinInt`),
  and each standard-library class the framework reaches takes it in both languages
  (`ProbeKotlinThrowable`, `KotlinThrowable` in Swift). Measured with Kotlin 2.4.0 and Swift 6.2.3,
  a spec class `KotlinInt` linked, and a caller's `let b: KotlinInt? = Client().boxed()`, of a
  function returning `Int?`, failed (`'KotlinInt' is ambiguous for type lookup in this context`).
  With a spec class `KotlinThrowable` the export named the standard library's class
  `KotlinThrowable_`, so a caller's `e.kotlinException as? KotlinThrowable` was `nil` for an
  exception the SDK threw. A spec class `KotlinEnum` kept its name until the SDK declared an enum,
  which brought the standard library's in and renamed the spec's `KotlinEnum_`. So every name whose
  first word is `Kotlin` is reserved, not only the names the framework reaches, which a declaration
  the spec adds can grow ([naming](naming.md#reserved-names)).
- Default arguments do not reach Swift, which is one reason models and request types are built
  through a builder rather than a constructor with defaults.
- An exception reaches Swift only as `@Throws` declares it: any other terminates the app, and a
  `suspend` function with no `@Throws` propagates cancellation alone. So every operation carries
  `@Throws(ApiException::class, OtherResponseException::class, CancellationException::class)`, which
  covers every class of the tree ([failures](errors.md)) and a response its method cannot return
  ([return shapes](returns.md)), and a Swift caller receives an `NSError` holding the exception, and
  reads its `failure` or `response`.
