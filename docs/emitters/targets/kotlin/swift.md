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
- Default arguments do not reach Swift, which is one reason models and request types are built
  through a builder rather than a constructor with defaults.
- An exception reaches Swift only as `@Throws` declares it: any other terminates the app, and a
  `suspend` function with no `@Throws` propagates cancellation alone. So every operation carries
  `@Throws(ApiException::class, OtherResponseException::class, CancellationException::class)`, which
  covers every class of the tree ([failures](errors.md)) and a response its method cannot return
  ([return shapes](returns.md)), and a Swift caller receives an `NSError` holding the exception, and
  reads its `failure` or `response`.
