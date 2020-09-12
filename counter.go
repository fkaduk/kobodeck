package main

import (
	"sync/atomic"
)

// Status holds all of our counters
type Status struct {
	Processed SafeCounter `json:"processed"`
	Downloaded SafeCounter `json:"downloaded"`
	Bytes SafeCounter `json:"bytes"`
	Deleted SafeCounter `json:"deleted"`
	Read SafeCounter `json:"read"`
}

// SafeCounter is safe to use concurrently.
// cargo-culted from https://gobyexample.com/atomic-counters
type SafeCounter struct {
	count uint32
}


// Inc increments the counter for the given key.
func (c *SafeCounter) Inc() {
	atomic.AddUint32(&c.count, 1)
}

// Add adds the value to the given key.
func (c *SafeCounter) Add(value uint32) {
	atomic.AddUint32(&c.count, value)
}

// Value returns the current value of the counter for the given key.
func (c *SafeCounter) Value() uint32 {
	return atomic.LoadUint32(&c.count)
}
