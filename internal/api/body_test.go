package api_test

import (
	"net/http"
	"testing"
)

// A body a route cannot read is refused with 400 and one sentence: the shape
// the route expected, then what the decoder said. These routes decode their
// body themselves, outside the branch, merge and stash families whose
// refusals are pinned beside them, so their sentence is pinned here — an
// unknown field is a client asking for something the route does not do, and
// the reply is what tells that client which field it got wrong.
func TestRoutesRefuseABodyTheyCannotRead(t *testing.T) {
	t.Parallel()

	handler, id, _ := openWorkingRepository(t)

	cases := []struct {
		route string
		body  string
		want  string
	}{
		{
			route: "/api/repos",
			body:  `{"paht":"/somewhere"}`,
			want:  `unreadable request body, a {"path": "…"} object is expected: json: unknown field "paht"`,
		},
		{
			route: "/api/repos/" + id + "/resolve",
			body:  `{"paths":["a.txt"],"sides":"ours"}`,
			want:  `unreadable request body, a {"paths": ["…"], "side": "ours"} object is expected: json: unknown field "sides"`,
		},
		{
			route: "/api/repos/" + id + "/mergetool/plan",
			body:  `{"file":"a.txt"}`,
			want:  `unreadable request body, a {"path": "…"} object is expected: json: unknown field "file"`,
		},
		{
			route: "/api/repos/" + id + "/mergetool",
			body:  `{"file":"a.txt"}`,
			want:  `unreadable request body, a {"path": "…"} object is expected: json: unknown field "file"`,
		},
		{
			route: "/api/repos/" + id + "/commit",
			body:  `{"subject":"first"}`,
			want:  `unreadable request body, a {"message": "…"} object is expected: json: unknown field "subject"`,
		},
		{
			route: "/api/repos/" + id + "/commit",
			body:  `{"message":`,
			want:  `unreadable request body, a {"message": "…"} object is expected: unexpected EOF`,
		},
	}

	for _, testCase := range cases {
		response := postJSON(t, handler, testCase.route, testCase.body)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s %s: status = %d, want 400: %s", testCase.route, testCase.body, response.Code, response.Body)
			continue
		}
		if got := decode[wireError](t, response).Error.Message; got != testCase.want {
			t.Errorf("%s %s:\n got %s\nwant %s", testCase.route, testCase.body, got, testCase.want)
		}
	}
}
