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
  - `Client` and each sub-client, in the unit holding it, and in every unit what the runtime's
    templates declare, though the runtime's own unit holds no spec name ([the runtime's
    unit](../../refine/output-units.md#the-runtimes-unit)): `ApiFailure`, `ApiException` and the
    exceptions under it ([failures](errors.md)), `Fault`, `OtherResponse`, `OtherResponseException`,
    `Response`, `EnvelopedResponse`, `RawBody`, `Optional`, `Duration`, `Authenticator`,
    `Transport`, and the opt-in annotations `AlphaApi`, `BetaApi` and `InternalSdkApi`
    ([stability](operations.md#stability)) among them, and on Multiplatform `Decimal` and
    `OffsetDateTime`. A file naming one beside a spec class of its name imports one under a name of
    its own ([imports](#imports)), so no generated file needs these reserved, while a caller's
    does: with Kotlin 2.4.0, a file importing a unit and the runtime's with `*` found a class both
    declared ambiguous where it named it ("overload resolution ambiguity between candidates"), so a
    spec class named `Response` would cost each such caller an explicit import.
  - Each name Kotlin's default imports give that a refine step, or a template rendering beside the
    spec's names, can write, such as `List`, `Map`, `String`, `Long`, `Unit`, `Exception` and
    `Result`, since a top-level declaration shadows a default import for its whole package: with a
    model `List` in a package, `List<String>` there failed ("No type arguments expected for 'class
    List : Any'"), and with a model `Exception`, a class extending `Exception` did. Each step and
    template declares those it writes as it declares what it binds, and the set is the union of
    what the steps and the templates rendering beside the spec's names declare, for the target's
    version, never what one unit's code happens to write, so adding an operation renames no model.
    The runtime's templates render into its unit alone, so they add none, while the template
    rendering an operation's row of the runtime's settings renders beside the spec's names, in the
    operation's unit ([runtime policy](../../contract/runtime-policy.md#delivery)).
  - Each Windows device name above, in any letter case, as the JVM writes a top-level class to a
    file of its name, `Aux.class`, which Windows refuses whatever the extension.
  - On Multiplatform, in the Objective-C namespace every unit's declarations share, a nested one's
    name joined to its outer's ([Swift](swift.md)): each name above that the target declares, the
    runtime's among them, in every unit, bar `AlphaApi`, `BetaApi` and `InternalSdkApi`,
    annotation classes Kotlin/Native 2.4.0 leaves out of the header; the joined name of each
    public class the target
    nests in a type, such as `UserBuilder` and `UserCompanion` beside a model `User` and
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
- **A type**: `equals`, `hashCode`, `toString`, `Builder` and `Companion`.

The names a unit declares and a union's variants are compared by the path fold ([emit
rules](../../emit/rules.md),
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
a revision changes keeps the comparison its variants' names shipped under. A model's properties are
no class, so they keep names apart by case, and so do a class's methods, though kotlinc writes a
class for each `suspend` one, since it numbers them in their class: Kotlin 2.4.0 wrote
`Client$getA$1.class` and `Client$geta$2.class` for methods `getA` and `geta`, and both ran. A
failure kind's exception is a class too, named for its kind in `Policy.Errors`
([failures](errors.md)), and a name a template declares never yields, so two kinds that differ in
letter case alone would share a class file: measured with Kotlin 2.4.0 on a file system that ignores
case, `NotFoundException` and `NotfoundException` compiled to the one file
`NotFoundException.class`, and loading the first failed (`NoClassDefFoundError: p/NotFoundException
(wrong name: p/NotfoundException)`). So a kind is one name whatever its letter case, on every target
([what a layer writes](../../contract/runtime-policy.md#what-a-layer-writes)).

A type's name, a variant's, a package segment and a method's are the roles Kotlin writes into
paths, a method's in its continuation's class file, so each is held to the length cap
([naming](../../refine/naming.md#a-name-too-long-for-a-path-is-shortened)); a source file is named
for its class, so the cap holds it too, and a Gradle module's directory is its unit's name.

A group's package segment is its words in lowercase, joined with no separator (`useraccounts`), as
Go's package names are, and a partition's is the name the options give it. So two groups whose words
break differently render alike and collide ([output units](../../refine/output-units.md)), and no
two units differ in letter case alone, which a case-insensitive file system would make one
directory: two partitions' names that do are an options error, and a group whose name differs from a
partition's in letter case alone yields to it.

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
Kotlin allows on both platforms and which leaves the wire name alone; a name a scope reserves for
what the generated code binds ([reserved names](../../refine/reserved-names.md)), which backticks
would not set apart, takes a trailing underscore.

## Imports

Every reference a Kotlin file writes begins with a name the file gives at its top level: one of its
own top-level declarations, or the name it imports a declaration under. A nested class is reached
through its outermost class (`Pet.Dog`) and a member through its receiver (`this.other`), so neither
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
String)`, another method's `error("boom")` called the operation and threw nothing. A class
implementing an interface does not see the interface's nested classes, while one extending a class
sees the superclass's, so what a class inherits counts as its own.

So the names a file gives at its top level differ from one another and from every name a nested
scope of the file declares or inherits: a class nested at any depth, the plugin's `Companion`, a
member, an enum entry, a type parameter, a parameter and a local. A declaration the file names
whose name one of them takes is imported under a name of its own, which the collision rule gives it
as it gives any name that yields
([collisions](../../refine/naming.md#collisions-and-the-names-a-generation-shipped)):
`import com.acme.shared.Dog as Dog2` beside a variant `Dog`, `import kotlin.collections.List as
List2` beside a variant `List`, `import kotlin.error as error2` beside an operation `error`, and
`import com.acme.users.Pet as Pet2` in the file declaring a union `Pet` with a variant `Pet`. The
declaration's own name then resolves nowhere in that file: with `import p.Dog as DogValue`,
`Dog("x")` there was an unresolved reference, though `p` declares `Dog`, so every reference to it in
the file goes through the alias. No declaration of the file yields to an import, since a caller
writes its classes and members by name, and its parameters too, in a named argument and as a Swift
argument label. Nothing is written qualified by its package.

An alias is its file's alone. The class files and the framework's header name each class by its own
name: Kotlin/Native 2.4.0 wrote `ProbeDog *value` for a wrapper holding a `Dog2`. With wrappers,
models and a model's builder written this way across two modules, a consumer in a third module and
another package built each wrapper from the type it wraps and a model from the models its properties
name, the same sources built for JS, and a Swift caller built `PetDog(value: Dog(name: "rex"))`.
Removing any one alias, or writing its declaration qualified, failed a build, though the one a
wrapper `Pet.List` took for a model `List` only in the consumer: without it the SDK built, its
wrapper holding itself. So the rule renames no declaration, and what
an additive change brings into a file moves at most which of its imports take an alias, which no
caller sees. On the golden spec (@bc6fad24, morphic @fa253fe19), 2,398 of the 3,136 variants of its
1,003 unions write their own wrapper's name in their value's type, in 837 unions, so those files
import 2,398 declarations under an alias; 1,372 of those variants wrap an anonymous type whose hint
is the variant's, each class counted as its words join in PascalCase. One variant is named as its
union, no model holds a type named `Builder` or `Companion`, and the 3 properties named `org` and
the 3 named `app` keep their names under a root that begins with either. T-55 holds it.

Four other designs were weighed, and each fails an input:

- **Writing the declaration qualified**, as this section once did for a name an import shares,
  resolves in a type but fails in an expression wherever a value named as the package's first
  segment is in scope, as in a model's body above. Kotlin has no form qualified from the root, as
  C#'s `global::` is, which no scope could take.
- **Renaming the nested class** where code inside it names a declaration of its name renames 2,398
  variants on the golden spec, each the name a caller writes (`Pet.Dog(...)`), and a variant the
  spec adds whose type names an existing variant's name renames that variant.
- **Reserving each package's first segment** in every scope that writes one renames a property or a
  parameter, which a caller names, for a fact elsewhere in its file.
- **Nesting nothing**, every wrapper and builder at the top level as `PetDog` and `UserBuilder`, as
  Go's are, still leaves the plugin's `Companion` nested in every model, so the rule above is needed
  all the same. It drops the nesting a Kotlin caller writes, and puts each wrapper among its unit's
  names, where 293 of the golden spec's would fold as a type in one unit does, so a wrapper's name,
  which a caller writes, would turn on the rest of its unit.
