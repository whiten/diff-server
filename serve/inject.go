// Package serve implements the Replicant http server. This includes all the Noms endpoints,
// plus a Replicant-specific sync endpoint that implements the server-side of the Replicant sync protocol.
package serve

import (
	"encoding/json"
	"net/http"

	"github.com/pkg/errors"
	zl "github.com/rs/zerolog"
	servetypes "roci.dev/diff-server/serve/types"
)

// inject inserts a client view into the cache. This is primarily useful for testing without
// having to have a data layer running.
func (s *Service) inject(w http.ResponseWriter, r *http.Request, l zl.Logger) {
	if !s.enableInject {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if r.Method != "POST" {
		unsupportedMethodError(w, r.Method, l)
		return
	}

	var req servetypes.InjectRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		clientError(w, http.StatusBadRequest, errors.Wrap(err, "Bad request payload").Error(), l)
		return
	}

	if req.AccountID == "" {
		clientError(w, http.StatusBadRequest, "Missing accountID", l)
		return
	}

	_, ok := lookupAccount(req.AccountID, s.accounts)
	if !ok {
		clientError(w, http.StatusBadRequest, "Unknown accountID", l)
		return
	}

	// TODO: auth

	if req.ClientID == "" {
		clientError(w, http.StatusBadRequest, "Missing clientID", l)
		return
	}

	db, err := s.GetDB(req.AccountID, req.ClientID)
	if err != nil {
		serverError(w, err, l)
		return
	}

	err = storeClientView(db, req.ClientViewResponse, l)
	if err != nil {
		serverError(w, err, l)
	}
}
