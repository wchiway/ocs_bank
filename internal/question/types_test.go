package question

import "testing"

func TestCacheIdentity(t *testing.T) {
	a, _ := (Question{Title: " q ", Options: "a b", Type: "判断题"}).Normalize()
	b, _ := (Question{Title: "q", Options: "a b", Type: "judgement"}).Normalize()
	if a.Key("x") != b.Key("x") {
		t.Fatal("normalization")
	}
	b.Options = "b a"
	if a.Key("x") == b.Key("x") {
		t.Fatal("options collided")
	}
	if a.Key("x") == a.Key("y") {
		t.Fatal("provider namespace collided")
	}
}
