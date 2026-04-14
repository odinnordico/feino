package structs

import (
	"reflect"
	"sync"
	"testing"
)

func TestNewLinkedList(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		list := NewLinkedList[int]()
		if list.Size() != 0 {
			t.Errorf("expected size 0, got %d", list.Size())
		}
		if !list.IsEmpty() {
			t.Error("expected list to be empty")
		}
	})

	t.Run("WithValues", func(t *testing.T) {
		list := NewLinkedList(1, 2, 3)
		if list.Size() != 3 {
			t.Errorf("expected size 3, got %d", list.Size())
		}
		expected := []int{1, 2, 3}
		if !reflect.DeepEqual(list.Values(), expected) {
			t.Errorf("expected %v, got %v", expected, list.Values())
		}
	})
}

func TestLinkedList_Push(t *testing.T) {
	t.Run("PushBack", func(t *testing.T) {
		list := &LinkedList[string]{}
		list.PushBack("a")
		list.PushBack("b")

		if list.Size() != 2 {
			t.Errorf("expected size 2, got %d", list.Size())
		}

		expected := []string{"a", "b"}
		if !reflect.DeepEqual(list.Values(), expected) {
			t.Errorf("expected %v, got %v", expected, list.Values())
		}
	})

	t.Run("PushFront", func(t *testing.T) {
		list := &LinkedList[string]{}
		list.PushFront("a")
		list.PushFront("b")

		expected := []string{"b", "a"}
		if !reflect.DeepEqual(list.Values(), expected) {
			t.Errorf("expected %v, got %v", expected, list.Values())
		}
	})
}

func TestLinkedList_Pop(t *testing.T) {
	t.Run("PopFront", func(t *testing.T) {
		list := NewLinkedList(1, 2)
		val, ok := list.PopFront()
		if !ok || val != 1 {
			t.Errorf("expected (1, true), got (%v, %v)", val, ok)
		}
		if list.Size() != 1 {
			t.Errorf("expected size 1, got %d", list.Size())
		}

		val, ok = list.PopFront()
		if !ok || val != 2 {
			t.Errorf("expected (2, true), got (%v, %v)", val, ok)
		}
		if !list.IsEmpty() {
			t.Error("expected list to be empty after popping all elements")
		}
	})

	t.Run("PopBack", func(t *testing.T) {
		list := NewLinkedList(1, 2)
		val, ok := list.PopBack()
		if !ok || val != 2 {
			t.Errorf("expected (2, true), got (%v, %v)", val, ok)
		}
		if list.Size() != 1 {
			t.Errorf("expected size 1, got %d", list.Size())
		}

		val, ok = list.PopBack()
		if !ok || val != 1 {
			t.Errorf("expected (1, true), got (%v, %v)", val, ok)
		}
	})

	t.Run("PopFromEmpty", func(t *testing.T) {
		list := &LinkedList[int]{}
		val, ok := list.PopFront()
		if ok || val != 0 {
			t.Errorf("expected (0, false), got (%v, %v)", val, ok)
		}
	})
}

func TestLinkedList_Peek(t *testing.T) {
	t.Run("PeekNotEmpty", func(t *testing.T) {
		list := NewLinkedList(10, 20)
		val, ok := list.Peek()
		if !ok || val != 10 {
			t.Errorf("expected (10, true), got (%v, %v)", val, ok)
		}
		if list.Size() != 2 {
			t.Error("Peek should not change the size")
		}
	})

	t.Run("PeekEmpty", func(t *testing.T) {
		list := &LinkedList[int]{}
		val, ok := list.Peek()
		if ok || val != 0 {
			t.Errorf("expected (0, false), got (%v, %v)", val, ok)
		}
	})
}

func TestLinkedList_Clone(t *testing.T) {
	original := NewLinkedList(1, 2, 3)
	clone := original.Clone()

	if clone.Size() != original.Size() {
		t.Error("clone size should match original")
	}

	if !reflect.DeepEqual(clone.Values(), original.Values()) {
		t.Errorf("clone values mismatch")
	}

	// Modify clone, original should stay the same
	clone.PushBack(4)
	if original.Size() != 3 {
		t.Error("original size changed after clone modification")
	}
}

func TestLinkedList_Concurrency(t *testing.T) {
	list := &LinkedList[int]{}
	var wg sync.WaitGroup
	numGoroutines := 100
	opsPerGoroutine := 100

	for i := range numGoroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for j := range opsPerGoroutine {
				list.PushBack(g*1000 + j)
				list.Peek()
				list.Size()
			}
		}(i)
	}

	wg.Wait()
	if list.Size() != numGoroutines*opsPerGoroutine {
		t.Errorf("expected size %d, got %d", numGoroutines*opsPerGoroutine, list.Size())
	}
}

func TestLinkedList_Iterator(t *testing.T) {
	list := NewLinkedList(1, 2, 3)

	t.Run("Forward", func(t *testing.T) {
		var result []int
		for val := range list.Iterator() {
			result = append(result, val)
		}
		expected := []int{1, 2, 3}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})

	t.Run("Reverse", func(t *testing.T) {
		var result []int
		for val := range list.ReverseIterator() {
			result = append(result, val)
		}
		expected := []int{3, 2, 1}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})
}
