package question

import "testing"

func TestParseAnswer(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{`{"answer":"O'Reilly"}`, "O'Reilly"}, {`{"anwser":"对"}`, "对"}, {"```json\n{\"answer\":\"a###b\"}\n```", "a###b"}, {`{"answer":"x","anwser":"y"}`, ""}, {`{"answer":42}`, ""}, {`{"answer":" "}`, ""}, {`{answer:'x'}`, ""}, {`{"answer":"yes"} trailing`, ""}} {
		got, err := ParseAnswer(tc.raw)
		if tc.want == "" {
			if err == nil {
				t.Fatalf("accepted %s", tc.raw)
			}
		} else if err != nil || got != tc.want {
			t.Fatalf("%s => %q %v", tc.raw, got, err)
		}
	}
}
