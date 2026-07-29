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
