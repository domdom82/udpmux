package frame

import (
	"testing"
)

func Test_DecodeEncodeV1(t *testing.T) {
	f, err := NewHeaderV1("localhost:1234", []byte("hello world"))
	if err != nil {
		t.Fatal(err)
	}

	marshalled, err := EncodeV1(f)

	if err != nil {
		t.Fatal(err)
	}

	f2, err := DecodeV1(marshalled)
	if err != nil {
		t.Fatal(err)
	}

	f2EndpointStr := string(f2.Endpoint[:f2.EndpointLen])

	if f2EndpointStr != "localhost:1234" {
		t.Fatalf("expected endpoint %s, got %s", "localhost:1234", f2EndpointStr)
	}
}

func Test_DecodeEncodeV2(t *testing.T) {
	f := NewHeaderV2(uint32(42), []byte("hello world"))

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

func Test_DecodeEncodeV1asV2(t *testing.T) {
	f, err := NewHeaderV1("localhost:1234", []byte("hello world"))
	if err != nil {
		t.Fatal(err)
	}

	marshalled, err := EncodeV1(f)

	if err != nil {
		t.Fatal(err)
	}

	_, err = DecodeV2(marshalled)
	if err == nil {
		t.Fatal("expected error decoding V1 frame as V2, got nil")
	}
}

func Test_DecodeEncodeV2asV1(t *testing.T) {
	f := NewHeaderV2(uint32(42), []byte("hello world"))

	marshalled, err := EncodeV2(f)

	if err != nil {
		t.Fatal(err)
	}

	_, err = DecodeV1(marshalled)
	if err == nil {
		t.Fatal("expected error decoding V2 frame as V1, got nil")
	}
}
