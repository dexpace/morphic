# Swift

A Multiplatform SDK reaches iOS through the Objective-C export, which shapes these rules.

- A sealed interface is not exhaustive in a Swift `switch`, so Swift callers need a `default` arm,
  and the generated docs say so.
- A name the export would mangle — `description` and `hash` (`NSObject` members), a name in the
  `init`, `new`, `copy` or `alloc` method families, a Swift keyword — gets an explicit Objective-C
  and Swift name from the naming policy (`@ObjCName`), so the Swift name is the policy's choice
  rather than the exporter's.
- Default arguments do not reach Swift, which is one reason models and request types are built
  through a builder rather than a constructor with defaults.
- An exception reaches Swift only as `@Throws` declares it: any other terminates the app, and a
  `suspend` function with no `@Throws` propagates cancellation alone. So every operation carries
  `@Throws(ApiException::class, OtherResponseException::class, CancellationException::class)`, which
  covers every class of the tree ([failures](errors.md)) and a response its method cannot return
  ([return shapes](returns.md)), and a Swift caller receives an `NSError` holding the exception, and
  reads its `failure` or `response`.
