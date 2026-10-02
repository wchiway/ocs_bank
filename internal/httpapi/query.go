package httpapi

import (
	"github.com/wchiway/ocs_bank/internal/question"
	"net/http"
	"net/url"
)

func urlValues(r *http.Request) (map[string]string, error) {
	v, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return nil, e
	}
	m := map[string]string{}
	for _, k := range []string{"title", "options", "type"} {
		if len(v[k]) > 1 {
			return nil, question.ErrInvalid
		}
		m[k] = v.Get(k)
	}
	return m, nil
}
