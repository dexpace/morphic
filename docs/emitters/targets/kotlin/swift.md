# Swift

A Multiplatform SDK reaches iOS through the Objective-C export, which shapes these rules.

- A sealed interface is not exhaustive in a Swift `switch`, so Swift callers need a `default` arm,
  and the generated docs say so.
- A name the export would mangle, or a Swift caller could not reach — a member `NSObject` gives
  every exported class (below), a method or an enum entry in the `alloc`, `copy`, `init`,
  `mutableCopy` or `new` method family, a Swift keyword, a name another declaration exports — gets
  an explicit Objective-C and Swift name from the [naming policy](../../refine/naming.md)
  (`@ObjCName`), so the Swift name is the policy's choice rather than the exporter's. That name is
  the Kotlin name with the escape's trailing underscore ([the escape](naming.md#the-escape)),
  `zone_`, and for a method family the role's leading word before it ([leading
  words](../../refine/naming.md#an-identifier-its-role-cannot-begin-with-takes-a-leading-word)),
  `callCopy()` and an entry `valueCopy`: Objective-C reads a family from how a selector begins, so
  an underscore leaves the name in it, and Kotlin/Native 2.4.0 exported a method given `copy_` as
  `doCopy_` and an entry as `theCopy_`. `@ObjCName` is experimental in Kotlin 2.4.0, and a file
  using it without an opt-in fails to compile, so each such file opts in with
  `@file:OptIn(ExperimentalObjCName::class)`. No caller inherits that, and no signature carries it
  ([the language floor](overview.md#the-language-floor)). `@ObjCName` takes ASCII letters, digits
  and `_` alone, a digit never first, in its name and its Swift name alike (Kotlin/Native 2.4.0's
  `FirNativeObjCNameChecker`; `'@ObjCName' contains illegal characters`), so an explicit name for a
  declaration whose name holds a character past ASCII is written in ASCII (below).
- Every exported class extends `NSObject`, so a Swift caller sees `NSObject`'s members beside the
  class's own, and a member named as one is hidden by it, replaces it or breaks the header. Measured
  with Kotlin/Native 2.4.0 and Swift 6.2.3 against the macOS 26.2 SDK, a property, a method, a
  `suspend` method and an enum entry were named for each of the 119 names the runtime's `NSObject.h`
  and Foundation declare on `NSObject` and its protocol, and a method of one parameter for each of
  their one-parameter selectors, split at each word. A property or a method `dealloc` left a header
  no module builds from (`dealloc return type must be correctly specified as 'void' under ARC`), so
  no Swift file importing the framework compiled. A property `zone`, `copy`, `isProxy`,
  `retainCount` or `finalize` read as `NSObject`'s method, and `description` and `self` as the
  object's own; `hash`, `hashValue` and `superclass` were ambiguous, and `class`, `retain`,
  `release` and `autorelease` unavailable. A method `mutableCopy` called `NSObject`'s and crashed
  (`unrecognized selector`), while one named `zone` compiled under `NSObject`'s own selector,
  replacing it for whatever else calls it. An enum entry named for a parameterless member, an
  instance's or the class's, read as that member or not at all, `zone`, `load`, `initialize` and
  `version` among them (`instance member 'hash' cannot be used on type`). A method whose selector
  the export formed as one of theirs, `responds(toSelector:)` or `perform(selector:)`, was imported
  under `NSObject`'s Swift name (`incorrect argument label in call (have 'toSelector:', expected
  'to:')`). So the policy holds a table of the options version, read from that SDK as the Swift type
  names are ([naming](naming.md#reserved-names)): each selector `NSObject` and its protocol declare,
  the class's or an instance's, the members Swift adds (`hashValue`), and `new`, which the export
  renames in a property too. A property, or a parameterless method that is not `suspend`, takes an
  explicit name where its name is a parameterless instance selector of the table or one of those
  members; an enum entry where its Swift name is a parameterless selector, the class's or an
  instance's, or one of those members; and a method with parameters where the selector the export
  forms for it, its name, its first parameter's capitalized and each later parameter's, is in the
  table, which a `suspend` method's, ending in `completionHandler:`, never is. With such names a
  member of each kind above was reached, and `hash`, `copy()` and `responds(to:)` still answered as
  `NSObject`'s. Comparing a property with every name of the table instead would rename `version`, a
  class method's name alone, and `value`, the first piece of `valueForKey:`, which a Swift caller
  read as written. Naming only what the export itself renames, `description` and `hash`, as this
  rule once did, left the rest. On the golden spec the rule gives 782 properties an explicit name,
  742 `description`, 14 `className`, 9 `class`, 9 `hash`, 5 `zone`, 2 `self` and 1 `copy`, and 80
  enum entries, 20 `load`, 19 `copy` and 12 `zone` among them, and no operation's method, while its
  847 properties `value` and 98 `version` keep their names (spec @bc6fad24, morphic @fa253fe19, each
  name its words in camel case). T-19 holds it.
- The export renames an enum entry in camel case, splitting it at each `_`, so it loses a break
  letter case cannot mark
  ([naming](../../refine/naming.md#a-break-letter-case-cannot-mark-takes-an-underscore)): measured
  with Kotlin 2.4.0, entries `VALUE_1_0` and `VALUE_10` both became `value10`, the one the class
  declares second taking a trailing `_`, so which value a Swift caller's `value10` named followed
  their order. An entry whose words hold such a break takes an explicit name, its words cased as a
  camel role cases them, and with one `VALUE_1_0` exported as `value1_0` beside `VALUE_10`'s
  `value10`. Entries `A_日本` and `A日本` exported as `a日本` and `a日本_`, the second in declaration order
  taking the `_`, and with `A_日本`'s explicit name, which is in ASCII (below), as `a_u65e5u672c` and
  `a日本`.
- A declaration reaches Swift by its Objective-C name wherever the `swift_name` the export writes
  for it would hold a character past ASCII, in or out of backticks, since clang drops that
  attribute: in the declaration's own name (`'swift_name' attribute has invalid identifier for the
  base name`), in a parameter's (`for the parameter name`) or in the name of a class it is nested in
  (`for the context name`). Measured with Kotlin 2.4.0 and Swift 6.2.3, a class `A日本` was `ProbeA日本`
  in Swift, `A日本()` failing (`cannot find 'A日本' in scope`), the `Builder` it nests
  `ProbeA日本Builder`, and a class `Inनाम` nested in an ASCII `Outer` `ProbeOuterInनाम`, none keeping
  its nesting; a property `名前` and an enum entry kept their names, and a constructor its labels; and
  an ASCII class kept its Swift name beside a member `café`. For a class, a property and an entry
  that Objective-C name is the Swift name, which the policy reads as the export forms it, and
  resolves in the namespace every unit shares (below).
- A method whose name or a parameter's is past ASCII has a selector the export forms, the method's
  name, its first parameter's capitalized and each later parameter's, and a Swift name Swift derives
  from the selector by rules of its own. Measured so, `取得(値:数:)` was `取得値(_:数:)`, so `取得(値: 1)`
  failed, an ASCII `fetch(id:名前:)` was `fetchId(_:名前:)`, a builder's `名前(value:)` `名前Value(_:)`, and
  a first piece holding a preposition was split at it, `listBy(id:名前:)` becoming `list(byId:名前:)`.
  So the policy fixes such a method's selector and states no Swift name for it: T-54 holds the
  selector, and reads the name a Swift caller writes from the framework it builds. A parameter keeps
  its name as its label, and an ASCII method's Swift name moves when it gains a parameter past
  ASCII, as it does on any new parameter, a default argument not reaching Swift (below).
- A declaration past ASCII that takes an explicit name by a rule here takes one in ASCII, the
  identifier its role renders with each character past ASCII written as Go writes one it cannot
  hold, `u` and its code point in lowercase hex
  ([naming](../../refine/naming.md#a-character-a-target-cannot-write-is-written-by-its-name)). So
  does one whose exported name would hold U+2E2F, which no header can: `@ObjCName("Au2e2f")` on a
  class `Aⸯ` built a module a Swift file called (`Au2e2f()`). Giving every declaration past ASCII an
  explicit ASCII name was not taken: it writes each such name in code points, `u540du524d` for `名前`,
  where the export's names read as the spec's. Nor was giving one to each parameter past ASCII,
  though it kept a method its Swift name (`fetch2(id:u540du524d:)` under `@ObjCName("u540du524d")`):
  a caller would read a label in code points beside a property in the spec's script, and a method
  named past ASCII would still be Swift's to name. Nor was writing those characters by name in the
  Kotlin name, which would cost a JVM caller the spec's name for Swift's sake.
- The export drops a declaration's package and joins a nested one's name to its outer's, so every
  unit's declarations, nested ones among them, share one namespace, and of two that share a name it
  appends `_` to one. Measured with Kotlin 2.4.0, within one module and across modules the framework
  exports, the runtime's `ApiException` in `p.runtime` exported as `ApiException_` beside a model
  `p.accounts.ApiException`; a model `PetDog` as `PetDog_` beside the class `Dog` that a sealed
  interface `Pet` nests, which Swift calls `PetDog` too; and a model `UserBuilder` as `UserBuilder_`
  in Objective-C beside `User.Builder`, whose Swift name keeps its nesting while every name in it is
  ASCII (above). Two declarations collide there when their exported names do, whichever units and
  types hold them, and a name the target declares is reserved there in every unit
  ([naming](naming.md#reserved-names)).
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
- The export gives a value class no declaration, so neither Kotlin target declares one
  ([below](#no-value-class)).
- Default arguments do not reach Swift, which is one reason models and request types are built
  through a builder rather than a constructor with defaults.
- An exception reaches Swift only as `@Throws` declares it: any other terminates the app, and a
  `suspend` function with no `@Throws` propagates cancellation alone. So every operation carries
  `@Throws(ApiException::class, OtherResponseException::class, CancellationException::class)`, which
  covers every class of the tree ([failures](errors.md)) and a response its method cannot return
  ([return shapes](returns.md)), and a Swift caller receives an `NSError` holding the exception, and
  reads its `failure` or `response`. A receiver's parse method carries
  `@Throws(InboundException::class)` for the same reason
  ([operations](operations.md#operations-the-api-sends)).
- `@Deprecated` reaches Swift as `__attribute__((deprecated("…")))` holding its message alone, so a
  Swift caller is shown the message as written and no `ReplaceWith`, which is why the message names
  each replacement in plain text ([deprecations](../../refine/deprecation.md)). Kotlin/Native 2.4.0
  writes it on a function, a property and a class's initializers alone: a Swift caller naming a
  deprecated class as a type, or an enum entry, which the header marks nowhere, is shown nothing. So
  the framework is built with `-Xexport-kdoc`, which writes a class's KDoc, its deprecation's text
  among it, above it in the header, the documentation a Swift caller's tools read, and an enum's
  KDoc names each deprecated entry with its text, since Kotlin/Native 2.4.0 exports no entry's KDoc.
  A `@RequiresOptIn` annotation does not reach Swift at all ([stability](operations.md#stability)).

## No value class

Neither Kotlin target declares a value class. Each form a value class would serve is a final class
holding one value, with a generated `equals`, `hashCode` and `toString` over it and a generated
serializer that writes the value as its wire form, never as an object holding it: an open enum and a
flags enum ([enums](types.md#enums)), a scalar's declaration, a redacting one and one over the
runtime's `Bytes` among them ([declarations over a root](types.md#a-declaration-over-a-root)), and
the runtime's `Decimal` ([Multiplatform](multiplatform.md)).

The Objective-C export gives a value class no declaration. Measured with Kotlin/Native 2.4.0 and
Swift 6.2.3, a `value class Status(val wire: String)` was absent from the framework's header and
every position of its type was `id`, `Any` in Swift, or the underlying number's own type, so a Swift
caller passed the string `"active"` or the number 7 where the Kotlin signature asks a `Status` or a
flags enum, reached no constant of either companion, could not compare two scalars over `Bytes`, and
read a sensitive scalar's value in clear, its redacting `toString` gone with the class. As final
classes each was a class of the header that a Swift caller named, constructed and compared with
`==`, two equal ones were one element of a `Set`, the constants were reached through `companion`, a
kept unlisted member was read through `wire`, the wrong argument types no longer compiled, and the
sensitive scalar printed `[REDACTED]` from `print`, an interpolation and `String(reflecting:)`.
Through Gradle on macOS arm64 (kotlinx-serialization-json 1.11.0), a model holding one of each
encoded them as `"status":"active"`, `"perms":3`, `"secret":"hunter2"`, `"blob":"AQID"` and
`"price":0.10`, and a map keyed by the enum's class under the member's text, and decoded back equal;
the same class left `@Serializable` with no serializer of its own encoded as `{"wire":"active"}`.

The JVM could keep the value class, and the two targets were weighed apart. On JVM 25 a model
holding a value class over `String` took 28 bytes where one holding the final class took 44, the 16
being the class's own object; in a list either took 20 for each element, a value class being boxed
there. Against that, a function taking a value class compiles to a mangled name
(`takeV-scETfWE(String)`) that Java cannot call, one returning it hands Java the bare `String`, a
sensitive one included, and the form is part of every signature that names the type, so an SDK could
never move between the two forms, or between the two targets, without breaking each binary built
against it. One form on both costs the 16 bytes and keeps one declaration for common code, for Java
and for Swift. T-58 holds this.
