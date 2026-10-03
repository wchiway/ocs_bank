package question

import (
	"context"
	"testing"
)

func TestValidateAnswer(t *testing.T) {
	options := "A. 苹果\r\nB. 香蕉\r\nC. 梨"
	for _, tc := range []struct{ typ, options, answer, want string }{
		{"single", options, "香蕉", "香蕉"}, {"single", options, "B", ""},
		{"single", options, "苹果###香蕉", ""}, {"single", options, "西瓜", ""},
		{"multiple", options, "香蕉 ### 苹果###香蕉", "香蕉###苹果"},
		{"multiple", options, "苹果###", ""}, {"multiple", options, "苹果###西瓜", ""},
		{"judgement", "", "TRUE", "对"}, {"judgement", "", "错误", "错"},
		{"judgement", "", "不确定", ""}, {"completion", "", "x###x", "x###x"},
		{"completion", "", "x### ###y", ""}, {"single", "", "答案", "答案"},
		{"single", "A. 多行\n续行\nB. 内容", "多行\n续行", "多行\n续行"},
		{"single", "A. 重复\nB. 重复", "重复", "重复"},
		{"essay", options, "解释###内容", "解释###内容"},
	} {
		t.Run(tc.typ+tc.answer, func(t *testing.T) {
			got, err := ValidateAnswer(Question{Type: tc.typ, Options: tc.options}, tc.answer)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("got %q %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestInvalidTypedAnswerIsNotCached(t *testing.T) {
	m := &memory{values: map[string]Result{}}
	s := service(t, m, generatorFunc(func(_ context.Context, _ Question) (Result, error) { return Result{Answer: "A"}, nil }), 1, 2)
	q := Question{Title: "pick", Type: "single", Options: "A. apple\nB. pear"}
	for range 2 {
		if _, err := s.Query(context.Background(), q); err != ErrProtocol {
			t.Fatal(err)
		}
	}
	if m.writes != 0 {
		t.Fatal("invalid answer cached")
	}
}
