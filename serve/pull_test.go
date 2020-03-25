package serve

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attic-labs/noms/go/types"
	"github.com/stretchr/testify/assert"

	"roci.dev/diff-server/db"
	"roci.dev/diff-server/kv"
	servetypes "roci.dev/diff-server/serve/types"
	"roci.dev/diff-server/util/time"
)

func TestAPI(t *testing.T) {
	assert := assert.New(t)
	defer time.SetFake()()

	tc := []struct {
		pullMethod  string
		pullReq     string
		authHeader  string
		expCVReq    *servetypes.ClientViewRequest
		CVResponse  servetypes.ClientViewResponse
		CVErr       error
		expPullResp string
		expPullErr  string
	}{
		// Unsupported method
		{"GET",
			`{"accountID": "accountID", "baseStateID": "00000000000000000000000000000000", "checksum": "00000000", "clientID": "clientid"}`,
			"",
			nil,
			servetypes.ClientViewResponse{},
			nil,
			``,
			"Unsupported method: GET"},

		// No client view to fetch from.
		{"POST",
			`{"accountID": "accountID", "baseStateID": "00000000000000000000000000000000", "checksum": "00000000", "clientID": "clientid"}`,
			"",
			nil,
			servetypes.ClientViewResponse{},
			nil,
			`{"stateID":"s3n5j759kirvvs3fqeott07a43lk41ud","lastMutationID":1,"patch":[{"op":"remove","path":"/"},{"op":"add","path":"/foo","value":"bar"}],"checksum":"c4e7090d"}`,
			""},

		// Successful client view fetch, with an auth header.
		{"POST",
			`{"accountID": "accountID", "baseStateID": "00000000000000000000000000000000", "checksum": "00000000", "clientID": "clientid"}`,
			"authtoken",
			&servetypes.ClientViewRequest{ClientID: "clientid"},
			servetypes.ClientViewResponse{ClientView: map[string]interface{}{"new": "value"}, LastMutationID: 2},
			nil,
			`{"stateID":"hoc705ifecv1c858qgbqr9jghh4d9l96","lastMutationID":2,"patch":[{"op":"remove","path":"/"},{"op":"add","path":"/new","value":"value"}],"checksum":"f9ef007b"}`,
			""},

		// Fetch errors out.
		{"POST",
			`{"accountID": "accountID", "baseStateID": "00000000000000000000000000000000", "checksum": "00000000", "clientID": "clientid"}`,
			"",
			&servetypes.ClientViewRequest{ClientID: "clientid"},
			servetypes.ClientViewResponse{ClientView: map[string]interface{}{"new": "value"}, LastMutationID: 2},
			errors.New("boom"),
			`{"stateID":"s3n5j759kirvvs3fqeott07a43lk41ud","lastMutationID":1,"patch":[{"op":"remove","path":"/"},{"op":"add","path":"/foo","value":"bar"}],"checksum":"c4e7090d"}`,
			""},

		// No accountID passed in.
		{"POST",
			`{"baseStateID": "00000000000000000000000000000000", "checksum": "00000000"}`,
			"",
			nil,
			servetypes.ClientViewResponse{},
			nil,
			``,
			"Missing accountID"},

		// Unknown accountID passed in.
		{"POST",
			`{"accountID": "bonk", "baseStateID": "00000000000000000000000000000000", "checksum": "00000000"}`,
			"",
			nil,
			servetypes.ClientViewResponse{},
			nil,
			``,
			"Unknown accountID"},

		// No clientID passed in.
		{"POST",
			`{"accountID": "accountID", "baseStateID": "00000000000000000000000000000000", "checksum": "00000000"}`,
			"",
			nil,
			servetypes.ClientViewResponse{},
			nil,
			``,
			"Missing clientID"},

		// Invalid baseStateID.
		{"POST",
			`{"accountID": "accountID", "baseStateID": "beep", "checksum": "00000000", "clientID": "clientid"}`,
			"",
			nil,
			servetypes.ClientViewResponse{},
			nil,
			``,
			"Invalid baseStateID"},

		// No baseStateID is fine (first pull).
		{"POST",
			`{"accountID": "accountID", "baseStateID": "", "checksum": "00000000", "clientID": "clientid"}`,
			"",
			&servetypes.ClientViewRequest{ClientID: "clientid"},
			servetypes.ClientViewResponse{ClientView: map[string]interface{}{"new": "value"}, LastMutationID: 2},
			nil,
			`{"stateID":"hoc705ifecv1c858qgbqr9jghh4d9l96","lastMutationID":2,"patch":[{"op":"remove","path":"/"},{"op":"add","path":"/new","value":"value"}],"checksum":"f9ef007b"}`,
			""},

		// Invalid checksum.
		{"POST",
			`{"accountID": "accountID", "baseStateID": "00000000000000000000000000000000", "checksum": "not", "clientID": "clientid"}`,
			"",
			nil,
			servetypes.ClientViewResponse{},
			nil,
			``,
			"Invalid checksum"},
	}

	for i, t := range tc {
		td, _ := ioutil.TempDir("", "")
		s := NewService(td, []Account{Account{ID: "accountID", Name: "accountID", Pubkey: nil}}, "")
		noms, err := s.getNoms("accountID")
		assert.NoError(err)
		db, err := db.New(noms.GetDataset("client/clientid"))
		assert.NoError(err)
		m := kv.NewMapFromNoms(noms, types.NewMap(noms, types.String("foo"), types.String("bar")))
		err = db.PutData(m.NomsMap(), types.String(m.Checksum().String()), 1 /*lastMutationID*/)
		assert.NoError(err)

		fcvg := fakeClientViewGet{resp: t.CVResponse, err: t.CVErr}
		if t.expCVReq == nil {
			s.cvg = nil
		} else {
			s.cvg = &fcvg
		}

		msg := fmt.Sprintf("test case %d: %s", i, t.pullReq)
		req := httptest.NewRequest(t.pullMethod, "/sync", strings.NewReader(t.pullReq))
		req.Header.Set("Content-type", "application/json")
		if t.authHeader != "" {
			req.Header.Set("Authorization", t.authHeader)
		}
		resp := httptest.NewRecorder()
		s.pull(resp, req)

		body := bytes.Buffer{}
		_, err = io.Copy(&body, resp.Result().Body)
		assert.NoError(err, msg)
		if t.expPullErr == "" {
			assert.Equal("application/json", resp.Result().Header.Get("Content-type"))
			assert.Equal(t.expPullResp+"\n", string(body.Bytes()), msg)
		} else {
			assert.Regexp(t.expPullErr, string(body.Bytes()), msg)
		}
		if t.expCVReq != nil {
			assert.True(fcvg.called)
			assert.Equal(*t.expCVReq, fcvg.gotReq)
		}
		if t.authHeader != "" {
			assert.Equal(t.authHeader, fcvg.gotAuth)
		}
	}
}

type fakeClientViewGet struct {
	resp servetypes.ClientViewResponse
	err  error

	called  bool
	gotReq  servetypes.ClientViewRequest
	gotAuth string
}

func (f *fakeClientViewGet) Get(req servetypes.ClientViewRequest, authToken string) (servetypes.ClientViewResponse, error) {
	f.called = true
	f.gotReq = req
	f.gotAuth = authToken
	return f.resp, f.err
}
