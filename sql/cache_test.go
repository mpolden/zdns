package sql

import (
	"reflect"
	"testing"

	"github.com/mpolden/zdns/cache"
)

func TestCache(t *testing.T) {
	data := "3980405151 1578680472 00000100000100000000000003777777076578616d706c6503636f6d0000010001"
	v, err := cache.Unpack(data)
	if err != nil {
		t.Fatal(err)
	}
	client := testClient()
	c := NewCache(client)

	// Set and read
	c.Set(1, v)
	values := c.Read()
	if got, want := len(values), 1; got != want {
		t.Fatalf("len(values) = %d, want %d", got, want)
	}
	if got, want := values[0], v; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// Reset and read
	c.Reset()
	values = c.Read()
	if got, want := len(values), 0; got != want {
		t.Fatalf("len(values) = %d, want %d", got, want)
	}

	// Insert, remove and read
	c.Set(1, v)
	c.Set(2, v)
	c.Evict(1)
	values = c.Read()
	if got, want := len(values), 1; got != want {
		t.Fatalf("len(values) = %d, want %d", got, want)
	}
}
