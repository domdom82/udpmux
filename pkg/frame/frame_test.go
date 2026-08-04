package frame

import (
	"testing"
)

func Test_DecodeEncode(t *testing.T) {
	f, err := NewHeader("localhost:1234")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("hello world")
	f.Length = uint16(len(payload))

	marshalled, err := Encode(f)

	if err != nil {
		t.Fatal(err)
	}

	f2, err := Decode(marshalled)
	if err != nil {
		t.Fatal(err)
	}

	f2EndpointStr := string(f2.Endpoint[:f2.EndpointLen])

	if f2EndpointStr != "localhost:1234" {
		t.Fatalf("expected endpoint %s, got %s", "localhost:1234", f2EndpointStr)
	}

}

func Test_DecodeEncodeV2(t *testing.T) {
	f := NewHeaderV2(uint16(42))
	payload := []byte("hello world")
	f.Length = uint16(len(payload))

	marshalled, err := EncodeV2(f)

	if err != nil {
		t.Fatal(err)
	}

	f2, err := DecodeV2(marshalled)
	if err != nil {
		t.Fatal(err)
	}

	if *f != *f2 {
		t.Fatalf("expected decoded frame %v to match original frame %v", f2, f)
	}

}
