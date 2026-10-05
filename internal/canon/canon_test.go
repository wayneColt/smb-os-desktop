package canon

import "testing"

func TestKeyOrderDoesNotChangeEncodingOrHash(t *testing.T) {
	a, err := Canonicalize([]byte(`{"b":1,"a":{"y":[1,2],"x":"<&>"}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Canonicalize([]byte("{ \"a\" : { \"x\":\"<&>\", \"y\":[1, 2] },\n \"b\": 1 }"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":{"x":"<&>","y":[1,2]},"b":1}`
	if string(a) != want || string(b) != want {
		t.Fatalf("canonical forms differ:\n%s\n%s\nwant %s", a, b, want)
	}
}

func TestNumbersKeepTheirLiteral(t *testing.T) {
	got, err := Canonicalize([]byte(`{"amount":412.50,"n":7}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"amount":412.50,"n":7}` {
		t.Fatalf("got %s", got)
	}
}

func TestStructAndMapHashTheSame(t *testing.T) {
	type in struct {
		Z string `json:"z"`
		A int    `json:"a"`
	}
	h1, _ := Hash(in{Z: "x", A: 1})
	h2, _ := Hash(map[string]any{"a": 1, "z": "x"})
	if h1 != h2 || len(h1) != 64 {
		t.Fatalf("hashes %s %s", h1, h2)
	}
	h3, _ := Hash(map[string]any{"a": 2, "z": "x"})
	if h3 == h1 {
		t.Fatal("a changed value must change the hash")
	}
}

func TestRejectsTrailingData(t *testing.T) {
	if _, err := Canonicalize([]byte(`{"a":1} {"b":2}`)); err == nil {
		t.Fatal("expected an error for two JSON values")
	}
}
