# Naming in Kotlin

## Reserved names

What each [scope](../../refine/reserved-names.md) reserves in Kotlin:

- **The units**: `buildSrc`, which Gradle refuses as a project's name wherever its directory is; the
  root project's name, since a module of that name, published under the root's Maven `group`, has
  the root's coordinates, so Gradle resolves the root's dependency on it to the root itself and
  fails the build on a cycle, which also makes a root project the options name `shared` or
  `runtime`, a module the target names and never renames, an options error (a root project `runtime`
  beside the runtime's module failed: "Circular dependency between the following tasks"); and the
  names Windows keeps for its devices, `con`, `prn`, `aux`, `nul`, and `com` or `lpt` with a digit,
  in any letter case, which Windows refuses as a directory's name, so that neither the module nor
  the directory kotlinc writes the package's classes to could be made on Windows. The names Gradle
  and its plugins keep at a build's root meet no unit, as no module sits there ([output
  units](overview.md#output-units)).
- **A unit**: what the target declares there and what a top-level declaration cannot take:
  - `Client`, each sub-client and `Receiver` ([operations the API
    sends](operations.md#operations-the-api-sends)), in the unit holding it, and in every unit what
    the runtime's templates declare, though the runtime's own unit holds no spec name ([the
    runtime's unit](../../refine/output-units.md#the-runtimes-unit)): `ApiFailure`, `ApiException`
    and the exceptions under it ([failures](errors.md)), `Fault`, `OtherResponse`,
    `OtherResponseException`, `Response`, `EnvelopedResponse`, `RawBody`, `Optional`, `Duration`,
    `Authenticator`, `Transport`, `InboundRequest`, `InboundCheck`, `OutboundResponse`, and the opt-in
    annotations
    `AlphaApi`, `BetaApi` and `InternalSdkApi` ([stability](operations.md#stability)) among them,
    and on Multiplatform `Decimal` and `OffsetDateTime`. A file naming one beside a spec class of
    its name imports one under a name of its own ([imports](#imports)), so no generated file needs
    these reserved, while a caller's does: with Kotlin 2.4.0, a file importing a unit and the
    runtime's with `*` found a class both declared ambiguous where it named it ("overload resolution
    ambiguity between candidates"), so a spec class named `Response` would cost each such caller an
    explicit import.
  - Each class Kotlin's default imports give a file on a platform the target builds for, such as
    `List`, `Result`, `Error`, `System` and, on Native, `Platform`, a table of the options version
    as the Swift names below are, read from the Kotlin release and the JDK it is measured on. A
    top-level declaration shadows a default import for its whole package, so generated code would
    lose those it writes: with a model `List` in a package, `List<String>` there failed ("No type
    arguments expected for 'class List : Any'"), and with a model `Exception`, a class extending
    `Exception` did. A caller's file importing the unit with `*` loses every one of them, since a
    star import ranks above a default one: with Kotlin 2.4.0, a unit declaring `System` made
    `System.currentTimeMillis()` there an unresolved reference, and one declaring `Error(message:
    String)` made `println(Error("boom"))` print the unit's class where it had printed
    `java.lang.Error: boom`, so a type a later revision adds would break a caller that compiled, or
    change what it does. Reserving only the names generated code writes, as the target once did,
    guards generated code and not its caller, and leaving a star import's conflicts to the caller
    was not taken for the reason the runtime's names above are reserved. On the golden spec, of the
    28,873 models, enums, unions and scalars that are no decorator (`ir.Decorated`), each a
    top-level declaration named for its `Canonical` or, with none, its `Hint`, as words joined in
    PascalCase, 1,772 are named for one of 11 such classes on the JVM or Native: 1,679 `Result`, 47
    `Metadata`, 17 `Error`, 12 `Target`, 4 `Unit`, 4 `System`, 3 `OnErrorAction`, 2 `Module`, 2
    `Platform`, 1 `Annotation` and 1 `Function`. Each takes the escape ([below](#the-escape)),
    `Result_`, and those of one unit sharing a name are then told apart as any two are, `Result_2`
    (spec @bc6fad24, morphic @fa253fe19, each name resolved by kotlinc 2.4.0 on the JVM and by
    Kotlin/Native 2.4.0 in a file importing nothing; `State` and `Shutdown`, which resolve to a
    declaration private in its file and one package-private, left out, as no caller reaches either,
    and a shape's declaration, whose name adds a word, not counted).
  - Each Windows device name above, in any letter case, as the JVM writes a top-level class to a
    file of its name, `Aux.class`, which Windows refuses whatever the extension.
  - On Multiplatform, in the Objective-C namespace every unit's declarations share, a nested one's
    name joined to its outer's ([Swift](swift.md)): each name above that the target declares, the
    runtime's among them, in every unit, bar `AlphaApi`, `BetaApi` and `InternalSdkApi`, annotation
    classes Kotlin/Native 2.4.0 leaves out of the header; the joined name of each public class the
    target nests in a type, such as `UserBuilder` and `UserCompanion` beside a model `User` and
    `PaymentUnknown` beside a union `Payment`; and the names the export gives classes of its own,
    `Base`, `MutableSet`, `MutableDictionary`, `Number` and each boxed number's, such as `Int`,
    `Double` and `ULong`.
  - On Multiplatform, among the Swift names a caller sees: every type name Swift's standard library
    and Foundation declare, which a Swift caller has in scope, a table of the options version as the
    naming tables are ([naming policy](../../refine/naming.md)), the target's own names included, so
    the runtime's `Optional`, `Duration` and `Decimal` take Swift names of their own; and the
    framework's own name, `Swift` and `Foundation`, which a caller writes to qualify a name
    (`Probe.Client`). A module a caller imports by choice, such as SwiftUI, reserves nothing, since
    a file importing it and the framework qualifies a name the two share. A declaration that meets a
    name of this bullet or the last only there keeps its Kotlin name and takes an explicit
    Objective-C and Swift name, so no JVM name moves for Swift's sake.
- **A union's variants**: beside `Unknown`, each class kotlinc or the serialization plugin nests in
  a sealed interface, `Companion`, `DefaultImpls` and `WhenMappings`, since a variant is a class
  nested there: with Kotlin 2.4.0, a variant `Companion` of a `@Serializable` union failed
  ("conflicting declarations"), a variant `DefaultImpls` of one declaring a method with a body
  failed ("duplicate JVM class name 'p/Pet$DefaultImpls'"), and a variant `WhenMappings` of one
  whose method switches over an enum of another file failed likewise (`'p/Pet$WhenMappings'`), each
  building once escaped.
- **A member's Swift name**, on Multiplatform: each member `NSObject` and its protocol give every
  exported class, a table of the options version as the Swift type names above are, compared with a
  property's, a method's and an enum entry's exported name as [Swift](swift.md) says. A member that
  meets one keeps its Kotlin name and takes an explicit Objective-C and Swift name.
- **A type**: `equals`, `hashCode`, `toString`, `Builder`, `Companion` and `jsonDepth`, the depth a
  value records ([nesting](models.md#nesting)), and `jsonValues`, the count it records beside it.

The names a unit declares, a union's variants and the methods an operation lowers to on a client or
a receiver are compared by the path fold ([emit rules](../../emit/rules.md),
[collisions](../../refine/naming.md#collisions-and-the-names-a-generation-shipped)), since the JVM
writes each class to a class file of its name, a nested one's joined to its outer's by `$`, and a
case-insensitive file system makes two that differ in letter case alone one file. With classes
`Setup` and `SetUp` in a package `p`, Kotlin 2.4.0 wrote one `p/Setup.class`, and with variants
`Card` and `CARD` of a sealed interface `Payment` one `p/Payment$Card.class`; each program built
without a diagnostic and failed to load the class it had lost (`NoClassDefFoundError: p/Payment$Card
(wrong name: p/Payment$CARD)`), as classes `Straße` and `Strasse` did. So one of each pair takes a
suffix, as one of two names that render alike does, and a source file named for its class is held
apart with it. A template writing a file in a unit, as the one writing each operation's row of the
runtime's settings does ([runtime policy](../../contract/runtime-policy.md#delivery)), names it for
a name it declares there, which the unit reserves, so no spec class's file meets it. An inclusive
union's variants are properties, not classes, and compare folded all the same, so a union whose kind
a revision changes keeps the comparison its variants' names shipped under. kotlinc names each class
it writes for a method's body after the method, and sets two apart only where their names differ in
letter case alone: Kotlin 2.4.0 wrote `Client$getA$1.class` and `Client$geta$2.class` for methods
`getA` and `geta`, but `Client$straße$1.class` alone for `straße` and `strasse`, and one file for
`café` composed and decomposed, and the program failed to load the class it had lost
(`NoClassDefFoundError: p/Client$straße$1 (wrong name: p/Client$strasse$1)`); a class it copies from
an inline function, `Client$mapA$$inlined$map$1`, is numbered within its method, so methods `mapA`
and `mapa` met as well. On the golden spec no two operations of one group fold alike unless their
words do, so this renames none (spec @bc6fad24, morphic @fa253fe19, each operation's words joined).
A model's properties are no class, and a builder's setters, named for them, hold no lambda, so both
keep names apart by case. A file holding a declaration that is no class, a `typealias`, gets a class
of kotlinc's named for the file, `MoneyKt` for `Money.kt`, which a spec class `MoneyKt` beside it
met ("duplicate JVM class name 'p/MoneyKt'"); so that file names it for the alias with
`@file:JvmName("Money")`, a name the unit already holds apart, which built beside `MoneyKt`. That
is the only class a unit's file gets from kotlinc, since none declares a function or a property at
its top level and each alias has a file of its own ([models](models.md#a-shape-that-is-an-alias)). A
failure kind's exception is a class too, named for its kind in `Policy.Errors`
([failures](errors.md)), and a name a template declares never yields, so two kinds that differ in
letter case alone would share a class file: measured with Kotlin 2.4.0 on a file system that ignores
case, `NotFoundException` and `NotfoundException` compiled to the one file
`NotFoundException.class`, and loading the first failed (`NoClassDefFoundError: p/NotFoundException
(wrong name: p/NotfoundException)`). So a kind is one name whatever its letter case, on every target
([what a layer writes](../../contract/runtime-policy.md#what-a-layer-writes)).

A type's name, a variant's, a package segment and a method's are the roles Kotlin writes into paths,
a method's in the class kotlinc writes for its body, so each is held to the length cap
([naming](../../refine/naming.md#a-name-too-long-for-a-path-is-shortened)); a source file is named
for its class, so the cap holds it too, and a Gradle module's directory is its unit's name.

A unit's name is its package segment, its module's project and artifact and that module's directory
alike, rendered as Go renders a package's ([Go](../go/naming.md#package-directories)): its words
joined with no separator, folded by the path fold, each character outside `a`–`z` and `0`–`9`
written as `u` and its hex code point, so `useraccounts`, `strasse` for `Straße` and `u65e5u672c`
for `日本`; a partition's is the name the options give it. A Maven artifact's name takes ASCII
letters, digits and `_-.` alone: Gradle 9.8.2 built a module `straße` and refused to publish it
(`artifactId (straße) is not a valid Maven identifier ([A-Za-z0-9_\-.]+)`), and published it once
its artifact was `strasse`. Escaping only the artifact, which published too, was not taken, as it
gives a unit two names, the package a caller imports and the artifact its build depends on. So two
groups whose words break differently render alike and collide ([output
units](../../refine/output-units.md)), as two that fold alike do; a name the options give a unit
that this rendering would change, two partitions' names that fold alike among them, is an options
error, and a group whose name folds as a partition's yields to it.

None of a hard keyword, `kotlin` and `java` is a units name. A hard keyword as a package segment
takes the escape below, which kotlinc accepts in a `package` and an `import` alike: every hard
keyword, backticked, compiles on the JVM, and `fun`, `object`, `when`, `is`, `in` and `typealias`
also compile for JS and macOS Native and link into a framework. A soft or modifier keyword (`value`,
`data`, `import`, `internal`) needs no escape there. kotlinc refuses `kotlin` as a package's first
segment ("Only the Kotlin standard library is allowed to use the 'kotlin' package"), and the JVM
refuses to load a class whose package begins with `java` (`SecurityException: Prohibited package
name`). Only a name the options give a unit can begin a package, the root's above all, so one that
begins with either is an options error ([target options](../../contract/options.md)), and so is a
root with a Windows device name for any segment, since a name the options give never yields. Each
behaviour this section states was measured with Kotlin 2.4.0, Gradle 9.8.2 and Corretto 25 on
darwin/arm64, but for the Windows device names, which Windows' own file-naming rules give.

## The escape

A Kotlin hard keyword used as a name (`class`, `object`, `true`) is escaped with backticks, which
Kotlin allows on both platforms and which leaves the wire name alone; any other name a scope
reserves ([reserved names](../../refine/reserved-names.md)), which backticks would not set apart,
takes a trailing underscore, as a type `Error_` does for the default import, a variant `Companion_`
and a unit `con_`. With Kotlin 2.4.0, classes `Error_`, `Result_`, `System_` and `List_` built, and
a caller importing their package with `*` still printed `java.lang.Error: boom` from `Error("boom")`
and reached `System`, `Result<Int>` and `List<String>`. An explicit Swift name takes the underscore
too, bar one in an Objective-C method family ([Swift](swift.md)). An identifier holding a character
the lexer refuses as it stands, a mark or a letter or digit newer than its tables, is written in
backticks too and keeps the character, wherever a file writes it, an import and a KDoc link among
them ([naming](../../refine/naming.md#a-character-a-target-cannot-write-is-written-by-its-name)).

## Imports

Every reference a Kotlin file writes begins with a name the file gives at its top level: one of its
own top-level declarations, or the name it imports a declaration under. A nested class is reached
through its outermost class (`Pet.Dog`), a member through its class's labelled receiver
(`this@User.other`) and a lambda's parameter by the name the lambda gives it, never `it`, so none
depends on what a scope between them declares. A top-level name is what a nearer scope can take:
inside a class, a class it nests hides every declaration of that name, its package's, an import's
and a default import's alike, and in an expression a package's first segment binds as any name does,
so writing a declaration qualified only moves the question to the segment. Measured with Kotlin
2.4.0 on the JVM: a variant `Pet.Dog(val value: Dog)` held itself and refused a model `Dog`
("argument type mismatch: actual type is 'Dog', but 'Pet.Dog' was expected"); a variant `Pet.List`
made a sibling's `List<String>` fail ("no type arguments expected"); a variant `Pet.Pet` took the
union's own name from every wrapper's supertype; the `Companion` the serialization plugin gives a
`@Serializable` class, declared or not, took a model `Companion` in its body; in a class with a
property `com`, `com.acme.runtime.Duration(1)` failed ("unresolved reference 'acme'"), where the
type `com.acme.runtime.Duration` resolved; and in a client with an operation `error(message:
String)`, another method's `error("boom")` called the operation and threw nothing. A lambda with a
receiver is a scope too: in a class `User(size = 42)`, `buildList { add(this.size) }` gave `[0]`,
the list's own, where `this@User.size` gave `[42]`; inside `flow { }`, `emit(1)` called the
collector's `emit` beside an operation `emit`; and a lambda's `it` hid a parameter `it`. A class
implementing an interface does not see the interface's nested classes, while one extending a class
sees the superclass's, so what a class inherits counts as its own.

So the names a file gives at its top level differ from one another and from every name a nested
scope of the file declares or inherits: a class nested at any depth, the plugin's `Companion` and
the `serializer()` it gives the class, a member, an enum entry, a type parameter, a parameter, a
local and each member of a lambda's receiver, which the template writing the lambda declares as it
declares what it binds. A declaration the file names whose name one of them takes is imported under
a name of its own, which the collision rule gives it as it gives any name that yields
([collisions](../../refine/naming.md#collisions-and-the-names-a-generation-shipped)): `import
com.acme.shared.Dog as Dog2` beside a variant `Dog`, `import kotlin.collections.List as List2`
beside a variant `List`, `import kotlin.error as error2` beside an operation `error`, and `import
com.acme.users.Pet as Pet2` in the file declaring a union `Pet` with a variant `Pet`. The
declaration's own name then resolves nowhere in that file: with `import p.Dog as DogValue`,
`Dog("x")` there was an unresolved reference, though `p` declares `Dog`, so every reference to it in
the file goes through the alias. Of two imports sharing a name, one the target's own code writes, a
library's or the runtime's, keeps it, and of two of the spec's declarations, the one whose ID sorts
first. No declaration of the file yields to an import, since a caller writes its classes and members
by name, and its parameters too, in a named argument and as a Swift argument label. Nothing is
written qualified by its package. A property takes no call unless its type declares `invoke`, so
comparing every member's name gives some imports an alias no reference needed, `kotlin.error` beside
a property `error`; one comparison is kept all the same, since such an alias costs no caller
anything.

An alias is its file's alone. The class files and the framework's header name each class by its own
name: Kotlin/Native 2.4.0 wrote `ProbeDog *value` for a wrapper holding a `Dog2`. With wrappers,
models and a model's builder written this way across two modules, a consumer in a third module and
another package built each wrapper from the type it wraps and a model from the models its properties
name, the same sources built for JS, and a Swift caller built `PetDog(value: Dog(name: "rex"))`.
Removing an alias failed a build, though the one a wrapper `Pet.List` took for a model `List` only
in the consumer, the SDK building with its wrapper holding itself, and without `error2` the client
built and its call to `error` called the operation; writing a declaration qualified instead failed
in an expression beside a property named as the package's first segment, and resolved in a type. A
KDoc link names its target as code does, labelled with the declaration's own name, `[Dog][Dog2]`:
Dokka 2.3.0 rendered a link `[Dog2]` as `Dog2` and took a bare `[Dog]` in the union's KDoc to the
wrapper `Pet.Dog`, while `[Dog][Dog2]` rendered `Dog` and opened the model's page. So the rule
renames no declaration, and what an additive change brings into a file moves at most which of its
imports take an alias, which no caller sees. On the golden spec, each declaration named as rule 2
names it in one unit and each class counted as its words join in PascalCase, the files of 250 of its
1,003 unions import 1,117 declarations under an alias: a variant takes the name of the type it
holds, while the type takes a suffix wherever another of its name comes first in ID order, as most
anonymous types sharing a hint do (spec @bc6fad24, morphic @fa253fe19). One variant is named as its
union, no model holds a type named `Builder` or `Companion`, and the 3 properties named `org` and
the 3 named `app` keep their names under a root that begins with either. T-55 holds it.

Four other designs were weighed, and each fails an input:

- **Writing the declaration qualified**, as this section once did for a name an import shares,
  resolves in a type but fails in an expression wherever a value named as the package's first
  segment is in scope, as in a model's body above. Kotlin has no form qualified from the root, as
  C#'s `global::` is, which no scope could take.
- **Renaming the nested class** where code inside it names a declaration of its name renames 1,117
  variants on the golden spec, each the name a caller writes (`Pet.Dog(...)`), and a variant the
  spec adds whose type names an existing variant's name renames that variant.
- **Reserving each package's first segment** in every scope that writes one renames a property or a
  parameter, which a caller names, for a fact elsewhere in its file.
- **Nesting nothing**, every wrapper and builder at the top level as `PetDog` and `UserBuilder`, as
  Go's are, still leaves the plugin's `Companion` nested in every model, so the rule above is needed
  all the same. It drops the nesting a Kotlin caller writes, and puts each wrapper among its unit's
  names, where 48 of the golden spec's, joined as `PetDog` is, fold as a declared type's name and
  1,454 as another wrapper's, so a wrapper's name, which a caller writes, would turn on the rest of
  its unit.
