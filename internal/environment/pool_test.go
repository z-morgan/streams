package environment

import "testing"

func TestPoolAllocateRelease(t *testing.T) {
	p := NewPool(4010, 3)

	port1, err := p.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if port1 != 4010 {
		t.Errorf("got port %d, want 4010", port1)
	}

	port2, err := p.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if port2 != 4011 {
		t.Errorf("got port %d, want 4011", port2)
	}

	port3, err := p.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if port3 != 4012 {
		t.Errorf("got port %d, want 4012", port3)
	}

	// Pool exhausted.
	_, err = p.Allocate()
	if err == nil {
		t.Fatal("expected error from exhausted pool")
	}

	// Release and re-allocate.
	p.Release(4011)
	port, err := p.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if port != 4011 {
		t.Errorf("got port %d, want 4011 (reused slot)", port)
	}
}

func TestPoolReleaseOutOfRange(t *testing.T) {
	p := NewPool(4010, 2)
	// Should not panic.
	p.Release(9999)
	p.Release(4009)
}

func TestPoolSize(t *testing.T) {
	p := NewPool(5000, 5)
	if p.Size() != 5 {
		t.Errorf("got size %d, want 5", p.Size())
	}
}
