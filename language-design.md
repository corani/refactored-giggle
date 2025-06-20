# Language Design: Syntax Overview

This document is a work in progress, summarizing the language syntax and semantics.

---

## 1. Constants and Variables

```odin
// Constants use '::'
PI :: 3.14159

// Variables use ':=' (type can be inferred or explicit)
count := 42
name : string = "hello"
```

---

## 2. Functions

```odin
// Function with default parameter and named argument
hello :: func(arg: int = 42) {
    message :: "Hello from compiler-%d!\n"
    printf(message, arg)
}

// Single-line pure function with return type inference
@(pure)
sum :: func(a, b: int) = a + b

// Named parameters in calls
hello(arg=99)
```

---

## 3. Attributes

```odin
// Attributes are placed before definitions, can be comma-separated
@(extern)
printf :: func(msg: string, arg: int)

@(export)
main :: func() -> int { ... }

@(pure, inline)
fast_add :: func(a, b: int) = a + b
```

---


## 4. Structs, Enums, and Type Aliases


```odin
// Structs
Point :: struct { x: int, y: int }

// Struct with default values
Config :: struct { host: string = "localhost", port: int = 8080 }

// Type aliases
Temperature :: int
Meters :: float
StringList :: []string

// Enums (simple)
Color :: enum { Red, Green, Blue }

// Enums (with data)
Result :: enum(int) {
    Ok(value: int),
    Error(msg: string, code: int)
}

// Usage
res := Result.Ok(42)                     // positional
err := Result.Error(msg="fail", code=1)  // named

// Generic option type
Option :: enum($T) {
    Some(T),    // name is optional
    None
}

opt := Option.Some(123)             // type parameter is inferred
none : Option(int) = Option.None()  // can't infer the type parameter here
```

---

## 5. Generics (Parametric Polymorphism)

```odin
// Generic struct
Stack :: struct($T) { data: []T, len: int }

// Generic function (type parameters inferred from arguments)
pair :: func(a: $A, b: $B) = (a, b)

// Usage with type inference
a_stack := Stack(data: []int{1,2,3}, len: 3)
p := pair(1, "foo") // $A=int, $B=string
```

---

## 6. Arrays and Slices

```odin
nums :: [4]int         // Fixed-size array
names :: []string      // Slice (dynamic array)

matrix :: [3][3]float  // Multi-dimensional array
```

---

## 7. Function Overloading

- Overloading is allowed if signatures differ by parameter types or count.
- Ambiguous calls result in a compiler error.

```odin
foo :: func(x: int)
foo :: func(x: string)
foo(42)      // calls int version
foo("bar")   // calls string version
```

---

## 8. Struct Instantiation (Constructor Style)

```odin
cfg := Config(port: 9090)
p := Point(x: 1, y: 2)
```

---


## 9. Minimal Keywords and Visibility

- Most features are implemented via attributes and type parameters, not keywords.
- This keeps the language extensible and user-friendly.
- All symbols are public by default. Use the `@(private)` attribute to restrict visibility within a module/package.

```odin
@(private)
helper_func :: func(x: int) = x * 2

@(private)
InternalType :: struct { ... }
```

---


## 10. Imports

Imports bring in complete packages. By default, the last element of the import path is used as the prefix. Use `as` to specify an alias in case of conflicts.

```odin
import math
import mylib/utils

x := math.sqrt(2)
y := utils.do_something()
```

```odin
import utils
import mylib/utils as mu

x := utils.do_something()
y := mu.do_something_else()
```

---

## 11. Example Program

```odin
package main

@(extern)
printf :: func(msg: string, arg: int)

hello :: func(arg: int = 42) {
    message :: "Hello from compiler-%d!\n"
    printf(message, arg)
}

@(pure)
sum :: func(a, b: int) = a + b

@(export)
main :: func() -> int {
    count := sum(11, 22)
    hello(arg=count)
    return 0
}
```

---



## 12. Implicit Context

Every function, lambda, and method has access to an implicit context value via a special identifier (e.g., `context`). This context is not passed explicitly, but is always available for logging, resource management, cancellation, etc.

- The context is function-local and changes are only visible down the call stack (not up).
- When a function or block modifies the context, the change is scoped to that call and its callees; the previous context is restored on return.
- This enables safe dependency injection and avoids surprising side effects.

### Example

```odin
foo :: func(x: int) {
    context.log("foo called")
    context.allocator = myAllocator
    bar() // bar and anything it calls see the new allocator
    // after bar returns, context.allocator is restored
}

// Lambdas also have access to context
f := func(x: int) = context.trace("lambda called")
```

---

## 13. Lambdas (Anonymous Functions)

Lambdas use the same syntax as function definitions, but with `:=` instead of `::`. You can assign them to variables or pass them directly as arguments.

```odin
// Assigning a lambda to a variable
add := func(a: int, b: int) = a + b

// Passing a lambda directly
result := map([1,2,3], func(x: int) = x * 2)

// With block body
printer := func(msg: string) {
    printf(msg)
}
```

---

## 13. Destructuring Assignment

Destructuring allows you to unpack values from tuples, function returns, and enums directly into variables. Only positional destructuring with (a, b) is supported.

### Unnamed (Positional) Tuples

```odin
t := (1, 2)
(a, b) := t         // a = 1, b = 2
```

### Named Tuples

```odin
point := (x: 5, y: 7)
(a, b) := point     // a = 5, b = 7 (by position)

// Access by name
xval := point.x     // xval = 5
yval := point.y     // yval = 7
```

### Function Return Values

```odin
get_pair :: func() = (10, 20)
(x, y) := get_pair() // x = 10, y = 20

get_point :: func() = (x: 3, y: 4)
(x, y) := get_point() // x = 3, y = 4
```

### Pattern Matching on Tuples

```odin
switch t {
    case (a, b):
        // a = 1, b = 2
}

switch point {
    case (x, y):
        // x = 5, y = 7
}

// rebinding the fields to new names
switch point {
    case (x as a, y as b):
        // a = 5, b = 7
}
```

### Enum (Tagged Union) Destructuring (Pattern Matching)

```odin
switch res {
    case Result.Ok(value):
        // use value
    case Result.Error(msg, code):
        // use msg, code
    case Result.Pair(x as a, y as b):
        // use a, b
}

// Or single-variant destructuring
if Result.Ok(value) := res {
    // use value
}
```

### Option Type Destructuring

```odin
switch opt {
    case Option.Some(value):    // use value
    case Option.None:           // handle none
}

if Option.Some(x) := opt {
    // use x
}
```

---
