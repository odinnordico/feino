package structs

import (
	"fmt"
	"iter"
	"strings"
	"sync"
)

type Node[T any] struct {
	Value T
	Next  *Node[T]
	Prev  *Node[T]
}

type LinkedList[T any] struct {
	mu   sync.RWMutex
	head *Node[T]
	tail *Node[T]
	size int
}

func NewLinkedList[T any](values ...T) *LinkedList[T] {
	list := &LinkedList[T]{}
	for _, value := range values {
		list.PushBack(value)
	}
	return list
}

func (l *LinkedList[T]) PushBack(value T) {
	l.mu.Lock()
	defer l.mu.Unlock()

	newNode := &Node[T]{
		Value: value,
	}
	if l.head == nil {
		l.head = newNode
		l.tail = newNode
	} else {
		l.tail.Next = newNode
		newNode.Prev = l.tail
		l.tail = newNode
	}
	l.size++
}

func (l *LinkedList[T]) PushFront(value T) {
	l.mu.Lock()
	defer l.mu.Unlock()

	newNode := &Node[T]{
		Value: value,
	}
	if l.head == nil {
		l.head = newNode
		l.tail = newNode
	} else {
		newNode.Next = l.head
		l.head.Prev = newNode
		l.head = newNode
	}
	l.size++
}

func (l *LinkedList[T]) PopFront() (T, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.head == nil {
		var zero T
		return zero, false
	}
	value := l.head.Value
	l.head = l.head.Next
	if l.head == nil {
		l.tail = nil
	} else {
		l.head.Prev = nil
	}
	l.size--
	return value, true
}

func (l *LinkedList[T]) PopBack() (T, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.tail == nil {
		var zero T
		return zero, false
	}
	value := l.tail.Value
	l.tail = l.tail.Prev
	if l.tail == nil {
		l.head = nil
	} else {
		l.tail.Next = nil
	}
	l.size--
	return value, true
}

func (l *LinkedList[T]) Peek() (T, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.head == nil {
		var zero T
		return zero, false
	}
	return l.head.Value, true
}

func (l *LinkedList[T]) Size() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.size
}

func (l *LinkedList[T]) IsEmpty() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.size == 0
}

func (l *LinkedList[T]) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.head = nil
	l.tail = nil
	l.size = 0
}

func (l *LinkedList[T]) Values() []T {
	l.mu.RLock()
	defer l.mu.RUnlock()

	values := make([]T, 0, l.size)
	current := l.head
	for current != nil {
		values = append(values, current.Value)
		current = current.Next
	}
	return values
}

func (l *LinkedList[T]) Clone() *LinkedList[T] {
	l.mu.RLock()
	defer l.mu.RUnlock()

	newList := &LinkedList[T]{}
	current := l.head
	for current != nil {
		newList.PushBack(current.Value)
		current = current.Next
	}
	return newList
}

func (l *LinkedList[T]) String() string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var builder strings.Builder
	builder.WriteString("LinkedList{")
	current := l.head
	for current != nil {
		fmt.Fprintf(&builder, "%v", current.Value)
		if current.Next != nil {
			builder.WriteString(", ")
		}
		current = current.Next
	}
	builder.WriteString("}")
	return builder.String()
}

func (l *LinkedList[T]) Iterator() iter.Seq[T] {
	return func(yield func(T) bool) {
		l.mu.RLock()
		defer l.mu.RUnlock()

		current := l.head
		for current != nil {
			if !yield(current.Value) {
				break
			}
			current = current.Next
		}
	}
}

func (l *LinkedList[T]) ReverseIterator() iter.Seq[T] {
	return func(yield func(T) bool) {
		l.mu.RLock()
		defer l.mu.RUnlock()

		current := l.tail
		for current != nil {
			if !yield(current.Value) {
				break
			}
			current = current.Prev
		}
	}
}

func (l *LinkedList[T]) ForEach(fn func(T)) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	current := l.head
	for current != nil {
		fn(current.Value)
		current = current.Next
	}
}
