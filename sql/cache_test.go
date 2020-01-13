package sql

import (
	"reflect"
	"testing"

	"github.com/mpolden/zdns/cache"
)

func TestCache(t *testing.T) {
	data1 := "1 1578680472 00000100000100000000000003777777076578616d706c6503636f6d0000010001"
	v1, err := cache.Unpack(data1)
	if err != nil {
		t.Fatal(err)
	}
	data2 := "2 1578680472 00000100000100000000000003777777076578616d706c6503636f6d0000010001"
	v2, err := cache.Unpack(data2)
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(":memory:")
	if err != nil {
		panic(err)
	}
	c := NewCache(client)

	// Set and read
	c.Set(v1.Key, v1)
	values := c.Read()
	if got, want := len(values), 1; got != want {
		t.Fatalf("len(values) = %d, want %d", got, want)
	}
	if got, want := values[0], v1; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// Reset and read
	c.Reset()
	values = c.Read()
	if got, want := len(values), 0; got != want {
		t.Fatalf("len(values) = %d, want %d", got, want)
	}

	// Insert, remove and read
	c.Set(v1.Key, v1)
	c.Set(v2.Key, v2)
	c.Evict(v1.Key)
	values = c.Read()
	if got, want := len(values), 1; got != want {
		t.Fatalf("len(values) = %d, want %d", got, want)
	}

	// Replacing existing value changes order
	c.Reset()
	c.Set(v1.Key, v1)
	c.Set(v2.Key, v2)
	c.Set(v1.Key, v1)
	values = c.Read()
	if got, want := values[len(values)-1].Key, v1.Key; got != want {
		t.Fatalf("last Key = %d, want %d", got, want)
	}
}
