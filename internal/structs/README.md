# Package `internal/structs`

The `structs` package provides generic data structures shared across FEINO. Currently it contains a single type: a concurrent-safe generic doubly-linked list.

---

## LinkedList[T]

A thread-safe doubly-linked list generic over any type `T`. Used by the `model` package to hold `MessagePart` sequences in messages.

### Construction

```go
list := &structs.LinkedList[model.MessagePart]{}

// Or use the zero value directly:
var list structs.LinkedList[string]
```

### Mutation

```go
list.PushBack(value)     // append to tail
list.PushFront(value)    // prepend to head
front := list.PopFront() // remove and return head
back  := list.PopBack()  // remove and return tail
list.Clear()             // remove all elements
```

### Read

```go
head, ok := list.Peek()           // head without removal
size      := list.Size()
empty     := list.IsEmpty()
values    := list.Values()        // all values as a slice, head-to-tail
clone     := list.Clone()         // shallow copy of the list
str       := list.String()        // "[v1 v2 v3]" using fmt.Sprint per element
```

### Iteration

```go
// Forward iteration (head to tail).
for it := list.Iterator(); it.HasNext(); {
    val := it.Next()
}

// Reverse iteration (tail to head).
for it := list.ReverseIterator(); it.HasNext(); {
    val := it.Next()
}

// Functional iteration — callback receives each value.
list.ForEach(func(v model.MessagePart) {
    // process v
})
```

### Concurrency

All methods acquire a `sync.RWMutex` lock:

- **Write operations** (`PushBack`, `PushFront`, `PopFront`, `PopBack`, `Clear`) use a full write lock.
- **Read operations** (`Peek`, `Size`, `IsEmpty`, `Values`, `Clone`, `String`) use a read lock.
- **Iterators** take a read lock for the duration of iteration. Do not call mutation methods while an iterator is live.

---

## Best practices

- **Prefer `Values()`** when you need to process all elements — it creates a snapshot slice under a single lock, which is safer than concurrent iteration.
- **Use `Clone()`** when you need a point-in-time copy for read-only processing, especially before passing to a goroutine.
- **Do not hold an iterator across a mutation.** Obtain `Values()` first if you need to mutate while iterating.

---

## Extending

To add a new generic data structure:

1. Create a new file in `internal/structs/` (e.g., `ring_buffer.go`).
2. Write the implementation with a `sync.RWMutex` for concurrent safety.
3. Add comprehensive tests in `ring_buffer_test.go` covering concurrent access via `go test -race`.
