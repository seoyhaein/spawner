package actor

import "sync"

// Minimal FIFO mailbox (can be swapped to priority queue later).
// Non-blocking Enqueue; backpressure handled at dispatcher level.

type Mailbox[T any] struct {
	ch chan T
	// 추가
	closed chan struct{}
	once   sync.Once
}

func NewMailbox[T any](size int) *Mailbox[T] {
	return &Mailbox[T]{
		ch:     make(chan T, size),
		closed: make(chan struct{})}
}

// Enqueue 채널에 값을 넣어줌. close 문제 해결함.
func (m *Mailbox[T]) Enqueue(v T) bool {

	select {
	case <-m.closed:
		return false
	default:
	}
	select {
	case m.ch <- v:
		return true
	default:
		return false
	}
}

// Enqueue close 가 되었을때 사용하면 위험함.
// 작성자료 때문에 남겨둠.
/*func (m *Mailbox[T]) Enqueue(v T) bool {
	select {
	case m.ch <- v:
		return true
	default:
		return false
	}
}*/

// C readonly channel 리턴함.
func (m *Mailbox[T]) C() <-chan T { return m.ch }

// Close 한번만 close 되도록 구현함. // 데이터 채널 close는 소비자에서
func (m *Mailbox[T]) Close() {
	m.once.Do(func() {
		close(m.closed)
	})
}

// 자료 작성을 위해서 남겨 둠.
//func (m *Mailbox[T]) Close()      { close(m.ch) }
